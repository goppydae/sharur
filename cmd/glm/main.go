package main
 

import (
	"context"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/goppydae/gollm/extensions"
	"github.com/goppydae/gollm/internal/agent"
	"github.com/goppydae/gollm/internal/config"
	"github.com/goppydae/gollm/internal/contextfiles"
	"github.com/goppydae/gollm/internal/llm"
	pb "github.com/goppydae/gollm/internal/gen/gollm/v1"
	"github.com/goppydae/gollm/internal/modes"
	"github.com/goppydae/gollm/internal/modes/interactive"
	"github.com/goppydae/gollm/internal/prompts"
	"github.com/goppydae/gollm/internal/session"
	"github.com/goppydae/gollm/internal/service"
	"github.com/goppydae/gollm/internal/skills"
	"github.com/goppydae/gollm/internal/themes"
)
 
var version = "dev"
 

func main() {
	if err := rootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	var (
		// Model / provider
		model    string
		provider string
		apiKey   string
		models   string // comma-separated model patterns for cycling
		// Session flags
		continueSession bool
		resumeSession   string
		noSession       bool
		branchSession   string
		sessionPath     string
		sessionDir      string
		// System prompt flags
		systemPrompt       string
		appendSystemPrompt []string
		// Thinking
		thinking string
		// Tool flags
		noTools   bool
		toolsList string
		// Extension / skill / prompt flags
		extensionPaths []string
		skillPaths     []string
		promptTplPaths []string
		noExtensions   bool
		noSkills       bool
		noPromptTpls   bool
		// Context file flags
		noContextFiles bool
		// Theme flags
		themeName  string
		themePaths []string
		noThemes   bool
		// Startup flags
		verbose bool
		offline bool
		// Output mode (text|json|rpc)
		outputMode string
		// Output flags
		exportFile  string
		listModels  string
		showVersion bool
		// Safety flags
		dryRun bool
		// gRPC
		grpcAddr string
	)

	cmd := &cobra.Command{
		Use:   "glm [flags] [prompt...]",
		Short: "glm — local-first AI coding agent",
		Long: `gollm is a local-first AI agent with several modes:
  --mode tui     Interactive bubbletea TUI (default)
  --mode json    One-shot mode with JSONL output
  --mode grpc    gRPC server (multi-session, default addr :50051)`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: could not load config: %v\n", err)
				cfg = config.DefaultConfig()
			}

			// --version
			if showVersion {
				fmt.Printf("gollm version %s\n", version)
				return nil
			}

			// --list-models
			if cmd.Flags().Changed("list-models") {
				return runListModels(cfg, listModels)
			}

			// --export
			if exportFile != "" {
				res := resolveSession(cmd, continueSession, resumeSession, branchSession, sessionPath)
				return runExport(cfg, exportFile, res)
			}

			// Model override: support "provider/model" and "model:thinking" shorthands
			if model != "" {
				// Strip optional :thinking suffix first
				if ci := strings.LastIndexByte(model, ':'); ci > 0 {
					possibleThinking := model[ci+1:]
					switch possibleThinking {
					case "off", "minimal", "low", "medium", "high", "xhigh":
						if thinking == "" {
							thinking = possibleThinking
						}
						model = model[:ci]
					}
				}
				if idx := strings.IndexByte(model, '/'); idx >= 0 {
					cfg.Provider = model[:idx]
					cfg.Model = model[idx+1:]
				} else {
					cfg.Model = model
				}
			}
			if provider != "" {
				cfg.Provider = provider
			}
			if apiKey != "" {
				// Apply to whichever provider is active
				switch cfg.Provider {
				case "anthropic":
					cfg.AnthropicAPIKey = apiKey
				case "openai":
					cfg.OpenAIAPIKey = apiKey
				case "google":
					cfg.GoogleAPIKey = apiKey
				case "ollama":
					// Ollama might use it for auth in some setups
					cfg.OpenAIAPIKey = apiKey
				default:
					// Default to OpenAI-compatible for custom providers
					if cfg.OpenAIAPIKey == "" {
						cfg.OpenAIAPIKey = apiKey
					}
				}
			}
			if models != "" {
				cfg.Models = strings.Split(models, ",")
			}
			cfg.Models = expandModelPatterns(cfg)
			if thinking != "" {
				cfg.ThinkingLevel = thinking
			}
			if sessionDir != "" {
				cfg.SessionDir = sessionDir
			}
			if sessionPath != "" {
				cfg.SessionPath = sessionPath
			}
			if themeName != "" {
				cfg.Theme = themeName
			}
			if !noThemes {
				cfg.ThemePaths = append(cfg.ThemePaths, themePaths...)
			} else {
				cfg.ThemePaths = themePaths
			}
			cfg.Verbose = verbose
			cfg.Offline = offline
			if cmd.Flags().Changed("dry-run") {
				cfg.DryRun = dryRun
			}
			if grpcAddr != "" {
				cfg.GRPCAddr = grpcAddr
			}

			// Determine mode: --mode flag, then infer from args
			mode := cfg.Mode
			if outputMode != "" {
				switch outputMode {
				case "tui", "interactive":
					mode = "interactive"
				case "json", "print", "text":
					mode = "json"
				case "grpc":
					mode = "grpc"
				default:
					mode = outputMode
				}
			} else if len(args) > 0 && mode == "interactive" {
				// Bare prompt args without --mode → json mode (one-shot)
				mode = "json"
			}

			if cfg.Verbose {
				fmt.Printf("Startup: mode=%s, provider=%s, model=%s\n", mode, cfg.Provider, cfg.Model)
				if cfg.Offline {
					fmt.Println("Warning: Offline mode enabled.")
				}
			}

			cfg.NoContextFiles = noContextFiles

			// Auto-discover AGENTS.md / CLAUDE.md context files
			if !noContextFiles {
				if ctx := contextfiles.Load("."); ctx != "" {
					cfg.SystemPrompt += "\n\n" + ctx
				}
			}

			// System prompt overrides
			if systemPrompt != "" {
				cfg.SystemPrompt = systemPrompt
			}
			for _, extra := range appendSystemPrompt {
				if data, ferr := os.ReadFile(extra); ferr == nil {
					cfg.SystemPrompt += "\n\n" + string(data)
				} else {
					cfg.SystemPrompt += "\n\n" + extra
				}
			}

			// Tool overrides
			if noTools {
				cfg.DisabledTools = true
			}
			if toolsList != "" {
				cfg.EnabledTools = strings.Split(toolsList, ",")
			}

			// Extension/skill/prompt path overrides
			if !noExtensions {
				cfg.Extensions = append(cfg.Extensions, extensionPaths...)
			} else {
				cfg.Extensions = extensionPaths
			}
			if !noSkills {
				cfg.SkillPaths = append(cfg.SkillPaths, skillPaths...)
			} else {
				cfg.SkillPaths = skillPaths
			}
			if !noPromptTpls {
				cfg.PromptTemplatePaths = append(cfg.PromptTemplatePaths, promptTplPaths...)
			} else {
				cfg.PromptTemplatePaths = promptTplPaths
			}

			// Extension/skill/prompt/theme path defaults
			if !noSkills {
				cfg.SkillPaths = append(skills.DefaultDirs(), cfg.SkillPaths...)
			}
			if !noPromptTpls {
				cfg.PromptTemplatePaths = append(prompts.DefaultDirs(), cfg.PromptTemplatePaths...)
			}
			if !noThemes {
				cfg.ThemePaths = append(themes.DefaultDirs(), cfg.ThemePaths...)
			}

			cfg.Models = expandModelPatterns(cfg)

			cfg.NoExtensions = noExtensions
			cfg.NoSkills = noSkills
			cfg.NoPromptTemplates = noPromptTpls
			cfg.NoThemes = noThemes

			prov, err := config.BuildProvider(cfg)
			if err != nil {
				return fmt.Errorf("provider: %w", err)
			}
			registry := config.BuildToolRegistry(cfg)
			mgr := session.NewManager(cfg.SessionDir)
 
 			// Load extensions
 			var exts []agent.Extension
 			extLoader := extensions.NewLoader(cfg.Extensions, cfg.PythonPath)
 			exts = append(exts, extLoader.LoadOrLog()...)
 			defer extLoader.Cleanup()
 
 			// Load skills
 			skillDirs := append([]string{}, cfg.Extensions...)
 			skillDirs = append(skillDirs, skills.DefaultDirs()...)
 			skillDirs = append(skillDirs, cfg.SkillPaths...)
 			// If noSkills is true, we only use explicit SkillPaths and Extensions
 			if noSkills {
 				skillDirs = append([]string{}, cfg.Extensions...)
 				skillDirs = append(skillDirs, cfg.SkillPaths...)
 			}
 			skillLoader := extensions.NewSkillLoader(skillDirs)
 			if sks, lerr := skillLoader.Load(); lerr == nil {
 				exts = append(exts, sks...)
 			}
 
 			// Initialize Backend Service
 			svc := service.New(context.Background(), prov, registry, mgr, exts).WithConfig(cfg)
 
 			// Initialize In-Process Client
 			client, cleanup, err := service.NewInProcessClient(svc)
 			if err != nil {
 				return fmt.Errorf("init in-process client: %w", err)
 			}
 			defer cleanup()
 
 			preloadSession := resolveSession(cmd, continueSession, resumeSession, branchSession, sessionPath)
 			sessionID := ""
 
 			// Initial session setup via service
 			if !noSession {
 				var resp *pb.NewSessionResponse
 				var err error
 				switch {
 				case strings.HasPrefix(preloadSession, "branch:"):
 					id := strings.TrimPrefix(preloadSession, "branch:")
 					fresp, ferr := client.BranchSession(context.Background(), &pb.BranchSessionRequest{SessionId: id, MessageIndex: -1})
 					if ferr == nil {
 						sessionID = fresp.SessionId
 					}
 				case preloadSession == "continue":
					var lerr error
					sessionID, lerr = mgr.LatestWithMessages()
					if lerr != nil {
						fmt.Fprintf(os.Stderr, "warning: could not search for sessions to continue: %v\n", lerr)
					}
					if sessionID == "" {
						if mode != "interactive" && mode != "tui" {
							fmt.Fprintln(os.Stderr, "warning: no sessions with content found to continue; starting a new session")
						}
					}
 				case strings.HasPrefix(preloadSession, "resume:"):
 					sessionID = strings.TrimPrefix(preloadSession, "resume:")
 				case preloadSession != "" && preloadSession != "resume":
 					sessionID = preloadSession
 				}
 
 				if sessionID == "" {
 					resp, err = client.NewSession(context.Background(), &pb.NewSessionRequest{})
 					if err == nil {
 						sessionID = resp.SessionId
 					}
 				}
 			}
 
 			var handler modes.Handler
 			switch mode {
 			case "json", "print", "text":
 				handler = modes.NewPrintHandler(client, sessionID,
 					modes.PrintOptions{
 						NoSession:      noSession,
 						PreloadSession: preloadSession,
 						SystemPrompt:   cfg.SystemPrompt,
 						ThinkingLevel:  cfg.ThinkingLevel,
 						JSON:           mode == "json",
						DryRun:         dryRun,
 					})
 			case "grpc":
 				handler = modes.NewGRPCHandler(svc, cfg.GRPCAddr)
 			case "interactive":
 				handler = modes.NewInteractiveHandler(client, sessionID, cfg, cfg.Theme,
 					interactive.Options{
 						NoSession:      noSession,
 						PreloadSession: preloadSession,
 					})
 			default:
 				return fmt.Errorf("unknown mode: %s", mode)
 			}

			if cfg.Offline && (cfg.Provider == "openai" || cfg.Provider == "anthropic" || cfg.Provider == "google") {
				return fmt.Errorf("provider %q requires network access, but --offline is enabled", cfg.Provider)
			}

			return handler.Run(args)
		},
	}

	// Mode flags
	cmd.Flags().StringVar(&outputMode, "mode", "", "Mode: tui (default), json, grpc")

	// Model / provider
	cmd.Flags().StringVarP(&model, "model", "m", "", "Model (e.g. llama3, gpt-4o, anthropic/claude-sonnet-4-5)")
	cmd.Flags().StringVar(&provider, "provider", "", "Provider (ollama|openai|anthropic|llamacpp)")
	cmd.Flags().StringVar(&apiKey, "api-key", "", "API key (overrides env vars)")
	cmd.Flags().StringVar(&models, "models", "", "Comma-separated model patterns for Ctrl+P cycling")
	cmd.Flags().StringVar(&thinking, "thinking", "", "Thinking level: off, low, medium, high")

	// Session
	cmd.Flags().BoolVarP(&continueSession, "continue", "c", false, "Continue the last session")
	cmd.Flags().StringVarP(&resumeSession, "resume", "r", "", "Select a session to resume, or provide a session ID")
	cmd.Flags().Lookup("resume").NoOptDefVal = " " // allow bare --resume
	cmd.Flags().BoolVar(&noSession, "no-session", false, "Ephemeral mode: don't save the session")
	cmd.Flags().StringVar(&branchSession, "branch", "", "Branch from a session file or partial UUID into a new child session")
	// --fork is kept as a deprecated alias for --branch.
	cmd.Flags().StringVar(&branchSession, "fork", "", "Deprecated: use --branch")
	_ = cmd.Flags().MarkHidden("fork")
	cmd.Flags().StringVar(&sessionPath, "session", "", "Use a specific session file")
	cmd.Flags().StringVar(&sessionDir, "session-dir", "", "Directory for session storage and lookup")

	// System prompt
	cmd.Flags().StringVar(&systemPrompt, "system-prompt", "", "Override the system prompt")
	cmd.Flags().StringArrayVar(&appendSystemPrompt, "append-system-prompt", nil, "Append text or file to the system prompt (repeatable)")

	// Tools
	cmd.Flags().BoolVar(&noTools, "no-tools", false, "Disable all built-in tools")
	cmd.Flags().StringVar(&toolsList, "tools", "", "Comma-separated tools to enable (read,bash,edit,write,grep,find,ls)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Dry run mode: tools don't perform destructive actions")

	// Extensions / skills / prompts
	cmd.Flags().StringArrayVarP(&extensionPaths, "extension", "e", nil, "Load an extension file (repeatable)")
	cmd.Flags().BoolVar(&noExtensions, "no-extensions", false, "Disable extension discovery (-e paths still load)")
	cmd.Flags().StringArrayVar(&skillPaths, "skill", nil, "Load a skill file or directory (repeatable)")
	cmd.Flags().BoolVar(&noSkills, "no-skills", false, "Disable skills discovery")
	cmd.Flags().StringArrayVar(&promptTplPaths, "prompt-template", nil, "Load a prompt template file or directory (repeatable)")
	cmd.Flags().BoolVar(&noPromptTpls, "no-prompt-templates", false, "Disable prompt template discovery")

	// Context files
	cmd.Flags().BoolVar(&noContextFiles, "no-context-files", false, "Disable AGENTS.md and CLAUDE.md discovery")

	// Theme
	cmd.Flags().StringVar(&themeName, "theme", "", "Set the UI theme by name (dark, light, cyberpunk, synthwave)")
	cmd.Flags().StringArrayVar(&themePaths, "theme-path", nil, "Load a theme file or directory (repeatable)")
	cmd.Flags().BoolVar(&noThemes, "no-themes", false, "Disable theme discovery")

	// Startup behaviour
	cmd.Flags().BoolVar(&verbose, "verbose", false, "Force verbose startup output")
	cmd.Flags().BoolVar(&offline, "offline", false, "Disable startup network operations")

	// Output / info
	cmd.Flags().StringVar(&exportFile, "export", "", "Export session to HTML and exit")
	cmd.Flags().StringVar(&listModels, "list-models", "", "List available models (optional fuzzy search)")
	cmd.Flags().Lookup("list-models").NoOptDefVal = " " // allow bare --list-models
	cmd.Flags().BoolVarP(&showVersion, "version", "v", false, "Show version number")

	// gRPC
	cmd.Flags().StringVar(&grpcAddr, "grpc-addr", "", "gRPC server listen address (default :50051)")

	return cmd
}


// runListModels prints available model IDs from the configured provider.
func runListModels(cfg *config.Config, search string) error {
	prov, err := config.BuildProvider(cfg)
	if err != nil {
		return fmt.Errorf("provider: %w", err)
	}
	lister, ok := prov.(llm.ModelLister)
	if !ok {
		return fmt.Errorf("provider %q does not support listing models", cfg.Provider)
	}
	models, err := lister.ListModels()
	if err != nil {
		return fmt.Errorf("list models: %w", err)
	}
	search = strings.TrimSpace(strings.ToLower(search))
	for _, m := range models {
		if search == "" || strings.Contains(strings.ToLower(m), search) {
			fmt.Println(m)
		}
	}
	return nil
}

// runExport exports a session to an HTML file.
func runExport(cfg *config.Config, dest string, preloadSession string) error {
	mgr := session.NewManager(cfg.SessionDir)
	
	var sess *session.Session
	var err error

	switch {
	case strings.HasPrefix(preloadSession, "resume:"):
		id := strings.TrimPrefix(preloadSession, "resume:")
		sess, err = mgr.Load(id)
	case preloadSession == "continue":
		id, lerr := mgr.LatestWithMessages()
		if lerr != nil {
			return fmt.Errorf("search for session to continue: %w", lerr)
		}
		if id != "" {
			sess, err = mgr.Load(id)
		} else {
			fmt.Fprintln(os.Stderr, "warning: no sessions with content found to continue")
			err = fmt.Errorf("no sessions found to continue")
		}
	case preloadSession != "" && preloadSession != "resume":
		// Literal path or ID
		sess, err = mgr.Load(preloadSession)
		if err != nil {
			sess, err = mgr.LoadPath(preloadSession)
		}
	default:
		// Default to last session if no specific one requested
		ids, lerr := mgr.List()
		if lerr == nil && len(ids) > 0 {
			sess, err = mgr.Load(ids[len(ids)-1])
		} else {
			err = fmt.Errorf("no sessions found to export")
		}
	}

	if err != nil {
		return fmt.Errorf("load session for export: %w", err)
	}
	return exportSessionHTML(sess, dest)
}

func resolveSession(cmd *cobra.Command, continueSession bool, resumeSession string, branchSession string, sessionPath string) string {
	switch {
	case branchSession != "":
		return "branch:" + branchSession
	case continueSession:
		return "continue"
	case cmd.Flags().Changed("resume"):
		if resumeSession == " " || resumeSession == "" {
			return "resume"
		} else {
			return "resume:" + resumeSession
		}
	case sessionPath != "":
		return sessionPath
	default:
		return ""
	}
}

// exportSessionHTML renders a session to a simple HTML file.
func exportSessionHTML(sess *session.Session, dest string) error {
	var sb strings.Builder
	fmt.Fprintf(&sb, `<!DOCTYPE html>
<html lang="en">
<head><meta charset="UTF-8"><title>%s</title>
<style>
body{font-family:system-ui,sans-serif;max-width:860px;margin:2rem auto;padding:0 1rem;background:#0f172a;color:#e2e8f0}
.msg{margin:1rem 0;padding:1rem;border-radius:.5rem}
.user{background:#1e293b;border-left:3px solid #6366f1}
.assistant{background:#1e3a2f;border-left:3px solid #10b981}
.tool{background:#1e293b;border-left:3px solid #f59e0b;font-family:monospace;font-size:.85em}
pre{white-space:pre-wrap;word-break:break-word;margin:0}
h1{color:#6366f1}span.role{font-size:.8em;opacity:.6;text-transform:uppercase;letter-spacing:.05em}
</style></head>
<body>
<h1>%s</h1>
<p style="opacity:.5">ID: %s &mdash; %s</p>
`, html.EscapeString(sess.Name), html.EscapeString(sess.Name), html.EscapeString(sess.ID), sess.CreatedAt.Format("2006-01-02 15:04"))

	for _, m := range sess.Messages {
		cls := "msg " + m.Role
		fmt.Fprintf(&sb, `<div class="%s"><span class="role">%s</span><pre>%s</pre></div>`+"\n",
			html.EscapeString(cls), html.EscapeString(m.Role), html.EscapeString(m.Content))
	}
	sb.WriteString("</body></html>\n")

	if err := os.WriteFile(dest, []byte(sb.String()), 0o644); err != nil {
		return err
	}
	fmt.Printf("Exported to %s\n", dest)
	return nil
}

func expandModelPatterns(cfg *config.Config) []string {
	if len(cfg.Models) == 0 {
		return nil
	}

	var expanded []string
	seen := make(map[string]bool)

	for _, pat := range cfg.Models {
		pat = strings.TrimSpace(pat)
		if pat == "" {
			continue
		}

		// If it's a literal without globs, just add it
		if !strings.ContainsAny(pat, "*?") {
			if !seen[pat] {
				expanded = append(expanded, pat)
				seen[pat] = true
			}
			continue
		}

		// It's a glob. Resolve provider.
		provName := cfg.Provider
		pattern := pat
		if idx := strings.IndexByte(pat, '/'); idx >= 0 {
			provName = pat[:idx]
			pattern = pat[idx+1:]
		}

		// Build a temp config to get the provider
		tmpCfg := *cfg
		tmpCfg.Provider = provName
		prov, _ := config.BuildProvider(&tmpCfg)
		if prov == nil {
			if !seen[pat] {
				expanded = append(expanded, pat)
				seen[pat] = true
			}
			continue
		}

		lister, ok := prov.(llm.ModelLister)
		if !ok {
			// Cannot list, just keep the pattern as is (or skip?)
			if !seen[pat] {
				expanded = append(expanded, pat)
				seen[pat] = true
			}
			continue
		}

		models, err := lister.ListModels()
		if err != nil {
			// Skip on error
			continue
		}

		for _, m := range models {
			if matchGlob(pattern, m) {
				full := provName + "/" + m
				if !seen[full] {
					expanded = append(expanded, full)
					seen[full] = true
				}
			}
		}
	}

	return expanded
}

func matchGlob(pattern, name string) bool {
	// Simple glob matching: * matches anything
	// We'll use filepath.Match which handles * and ?
	matched, _ := filepath.Match(pattern, name)
	return matched
}

