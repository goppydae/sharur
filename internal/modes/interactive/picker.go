package interactive

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/sahilm/fuzzy"
)



// pickerPageSize is the number of items shown per page in the file picker.
const pickerPageSize = 10

// Update handles keyboard input for the picker.
func (p *Picker) Update(msg tea.KeyPressMsg) bool {
	if !p.Open {
		return false
	}

	key := msg.Key()
	switch key.Code {
	case tea.KeyPgUp:
		if p.Page > 0 {
			p.Page--
			p.Cursor = 0
		}
		return true

	case tea.KeyPgDown:
		if (p.Page+1)*pickerPageSize < len(p.Matches) {
			p.Page++
			p.Cursor = 0
		}
		return true

	case tea.KeyUp:
		if p.Cursor > 0 {
			p.Cursor--
			// Auto-page up when crossing page boundary
			if p.Cursor < p.Page*pickerPageSize {
				p.Page--
				p.Cursor = (p.Page+1)*pickerPageSize - 1
			}
		}
		return true

	case tea.KeyDown:
		if p.Cursor < len(p.Matches)-1 {
			p.Cursor++
			// Auto-page down when crossing page boundary
			if p.Cursor >= (p.Page+1)*pickerPageSize {
				p.Page++
				p.Cursor = p.Page * pickerPageSize
			}
		}
		return true
	}

	return false
}

// Reset clears the picker state.
func (p *Picker) Reset(kind pickerType, query string, items []string) {
	p.Open = true
	p.Kind = kind
	p.Query = query
	p.Items = items
	p.Matches = fuzzyFilter(items, query)
	p.Cursor = 0
	p.Page = 0
}

// Close hides the picker.
func (p *Picker) Close() {
	p.Open = false
	p.Matches = nil
}

// Selected returns the currently highlighted item.
func (p *Picker) Selected() (string, bool) {
	if !p.Open || len(p.Matches) == 0 {
		return "", false
	}
	return p.Matches[p.Cursor], true
}

// View renders the picker overlay.
func (p *Picker) View(style Style) string {
	if !p.Open {
		return ""
	}
	if len(p.Matches) == 0 {
		return style.Dim().PaddingLeft(0).Render("no matches")
	}

	start := p.Page * pickerPageSize
	end := start + pickerPageSize
	if end > len(p.Matches) {
		end = len(p.Matches)
	}

	pageItems := p.Matches[start:end]
	lines := make([]string, len(pageItems))
	for i, match := range pageItems {
		globalIdx := start + i
		displayTxt := match
		switch p.Kind {
		case pickerTypeSlash:
			displayTxt = "/" + match
		case pickerTypeSession:
			displayTxt = match
		}
		if globalIdx == p.Cursor {
			lines[i] = style.StatusWorking().Render("▸ " + displayTxt)
		} else {
			lines[i] = style.Muted().Render("  " + displayTxt)
		}
	}

	// Show page indicator if there are more results
	totalPages := (len(p.Matches) + pickerPageSize - 1) / pickerPageSize
	if totalPages > 1 {
		lines = append(lines, style.Dim().Render(
			fmt.Sprintf("page %d/%d (pgup/pgdn to scroll)", p.Page+1, totalPages)))
	}

	return strings.Join(lines, "\n")
}

// discoverFiles walks root and returns relative paths for both files and
// directories (directories have a trailing /), skipping hidden dirs and known
// large/binary directories.
func discoverFiles(root string) []string {
	skipDirs := map[string]bool{
		".git": true, "node_modules": true, "vendor": true,
		".cache": true, "dist": true, "build": true,
	}
	var entries []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path == root {
				return nil // skip the root entry itself
			}
			if strings.HasPrefix(d.Name(), ".") || skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return nil
			}
			entries = append(entries, rel+"/")
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		entries = append(entries, rel)
		return nil
	})
	return entries
}

// fuzzyFilter returns all entries matching query, best first.
// With an empty query, entries are sorted shallowest-first so top-level items
// appear before deeply nested ones.
func fuzzyFilter(files []string, query string) []string {
	if query == "" {
		sorted := append([]string(nil), files...)
		sort.SliceStable(sorted, func(i, j int) bool {
			di := strings.Count(sorted[i], "/")
			dj := strings.Count(sorted[j], "/")
			if di != dj {
				return di < dj
			}
			return sorted[i] < sorted[j]
		})
		return sorted
	}

	matches := fuzzy.Find(query, files)
	out := make([]string, len(matches))
	for i, m := range matches {
		out[i] = m.Str
	}
	return out
}

// atFragment finds an @-prefixed word at the end of the current line in val.
// Returns the fragment after @, the index of @ in val, and whether one was found.
func atFragment(val string) (query string, atIdx int, ok bool) {
	lastNL := strings.LastIndexByte(val, '\n')
	line := val[lastNL+1:]
	at := strings.LastIndexByte(line, '@')
	if at < 0 {
		return "", -1, false
	}
	fragment := line[at+1:]
	if strings.ContainsAny(fragment, " \t") {
		return "", -1, false
	}
	return fragment, lastNL + 1 + at, true
}

// replaceAtFragment inserts replacement after the @ at atIdx, keeping the @.
func replaceAtFragment(val string, atIdx int, replacement string) string {
	return val[:atIdx+1] + replacement
}

