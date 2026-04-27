package session

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type TreeScope int

const (
	ScopeSession TreeScope = iota
	ScopeProject
	ScopeGlobal
)

// TreeNode represents a node in the session tree.
type TreeNode struct {
	ID        string
	ParentID  *string
	Name      string
	Model     string
	Provider  string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	MsgCount     int
	FirstMessage string
	Children     []*TreeNode

	// Conversation-based fields
	Role     string
	Content  string
	IsActive bool

	// Lineage metadata (display only — not tree topology)
	ParentMessageIndex *int
	MergeSourceID      *string
	RebasedFrom        *string
}

// BuildTree loads all sessions and returns the roots of a session tree.
// If global is false, it only returns the tree containing currentID.
func (m *Manager) BuildTree(currentID string, scope TreeScope) ([]*TreeNode, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var projectDirs []string
	if scope == ScopeGlobal {
		entries, err := os.ReadDir(m.baseDir)
		if err == nil {
			for _, e := range entries {
				if e.IsDir() {
					projectDirs = append(projectDirs, filepath.Join(m.baseDir, e.Name()))
				}
			}
		}
	}
	if len(projectDirs) == 0 {
		projectDirs = []string{m.dir}
	}

	byID := make(map[string]*TreeNode)
	
	for _, pdir := range projectDirs {
		pstore := newStore(m.baseDir, pdir)
		ids, err := pstore.list()
		if err != nil {
			continue
		}

		for _, fileID := range ids {
			records, err := pstore.read(fileID)
			if err != nil {
				continue
			}

			for _, r := range records {
				if r.ID == "" {
					continue
				}

				normID := strings.TrimSpace(strings.ToLower(r.ID))
				node, ok := byID[normID]
				if !ok {
					node = &TreeNode{
						ID:       r.ID,
						ParentID: r.ParentID,
					}
					byID[normID] = node
				}

				if t, err := time.Parse(time.RFC3339Nano, r.Timestamp); err == nil {
					if node.CreatedAt.IsZero() || t.Before(node.CreatedAt) {
						node.CreatedAt = t
					}
					if t.After(node.UpdatedAt) {
						node.UpdatedAt = t
					}
				}

				switch r.Type {
				case TypeSession:
					if node.Name == "" {
						node.Name = r.ID[:8]
					}
					node.Role = "session"
					// Map the fileID to this node as well, so we can find it by filename.
					byID[strings.TrimSpace(strings.ToLower(fileID))] = node
				case TypeSessionInfo:
					node.Name = r.Name
					node.Role = "info"
					node.Content = r.Name
				case TypeMessage:
					if r.Message != nil {
						node.Role = r.Message.Role
						node.Content = r.Message.Content
						if node.FirstMessage == "" {
							node.FirstMessage = r.Message.Content
						}
					}
				case TypeModelChange:
					node.Model = r.Model
					node.Provider = r.Provider
					node.Role = "model"
					node.Content = r.Provider + "/" + r.Model
				case TypeThinkingLevelChange:
					node.Role = "thinking"
					node.Content = r.ThinkingLevel
				case TypeCompaction:
					node.Role = "compaction"
					node.Content = r.Summary
				case TypeBranchSummary:
					node.Role = "summary"
					node.Content = r.Summary
				case TypeLabel:
					node.Role = "label"
					node.Content = r.Label
				}
			}
		}
	}

	// Short-ID map for flexible matching (≥8 char prefix of the actual UUID).
	byShortID := make(map[string]*TreeNode)
	for id, node := range byID {
		if len(id) >= 8 {
			byShortID[id[:8]] = node
		}
	}

	// Link children
	allNodes := make(map[*TreeNode]bool)
	for _, n := range byID {
		allNodes[n] = true
	}

	var roots []*TreeNode
	for n := range allNodes {
		if n.ParentID != nil {
			pid := strings.TrimSpace(strings.ToLower(*n.ParentID))
			parent, ok := byID[pid]
			if !ok && len(pid) >= 8 {
				parent, ok = byShortID[pid[:8]]
			}
			if ok {
				parent.Children = append(parent.Children, n)
				continue
			}
		}
		roots = append(roots, n)
	}

	// Filter to current session lineage if requested
	if scope == ScopeSession && currentID != "" {
		root := findRoot(byID, byShortID, currentID)
		if root != nil {
			roots = []*TreeNode{root}
		} else {
			roots = nil
		}
	}

	// Mark active node
	currNode, ok := byID[strings.TrimSpace(strings.ToLower(currentID))]
	if !ok && len(currentID) >= 8 {
		currNode = byShortID[currentID[:8]]
	}
	if currNode != nil {
		currNode.IsActive = true
	}

	// Sort roots by UpdatedAt descending (most recent first)
	sort.Slice(roots, func(i, j int) bool {
		return roots[i].UpdatedAt.After(roots[j].UpdatedAt)
	})

	// Sort children chronologically (ascending)
	for _, n := range byID {
		sort.Slice(n.Children, func(i, j int) bool {
			return n.Children[i].UpdatedAt.Before(n.Children[j].UpdatedAt)
		})
	}

	return roots, nil
}

func findRoot(byID map[string]*TreeNode, byShortID map[string]*TreeNode, id string) *TreeNode {
	curr := id
	visited := make(map[string]bool)
	var last *TreeNode
	for curr != "" {
		normID := strings.TrimSpace(strings.ToLower(curr))
		if visited[normID] {
			break
		}
		visited[normID] = true

		n, ok := byID[normID]
		if !ok && len(normID) >= 8 {
			n, ok = byShortID[normID[:8]]
		}

		if !ok {
			break
		}
		last = n
		if n.ParentID != nil {
			curr = *n.ParentID
		} else {
			curr = ""
		}
	}
	return last
}


// GutterInfo tracks vertical branch lines for descendants.
type GutterInfo struct {
	Position int
	Show     bool
}

// FlatNode represents a node in the flattened tree list with layout metadata.
type FlatNode struct {
	Node               *TreeNode
	Indent             int
	ShowConnector      bool
	IsLast             bool
	Gutters []GutterInfo
}

// FlattenTree returns a depth-first flat list of all tree nodes with their depth and prefix.
func FlattenTree(roots []*TreeNode) []FlatNode {
	var result []FlatNode

	multipleRoots := len(roots) > 1

	var walk func(n *TreeNode, indent int, justBranched bool, isLast bool, gutters []GutterInfo)
	walk = func(n *TreeNode, indent int, justBranched bool, isLast bool, gutters []GutterInfo) {
		// Pi-mono rule: show connector if parent branched or it's a virtual root child
		showConnector := justBranched

		result = append(result, FlatNode{
			Node:          n,
			Indent:        indent,
			ShowConnector: showConnector,
			IsLast:        isLast,
			Gutters:       gutters,
		})

		children := n.Children
		multipleChildren := len(children) > 1

		// Calculate child indent following pi-mono rules:
		// - If parent branches: children get +1
		// - If it's the first generation after a branch: +1 for visual grouping
		// - Otherwise: stay flat
		childIndent := indent
		if multipleChildren {
			childIndent = indent + 1
		} else if justBranched && indent > 0 {
			childIndent = indent + 1
		}

		for i, child := range children {
			childIsLast := i == len(children)-1
			
			// Build child gutters
			var childGutters []GutterInfo
			if len(gutters) > 0 {
				childGutters = make([]GutterInfo, len(gutters))
				copy(childGutters, gutters)
			}
			
			// If this node showed a connector, add a gutter for descendants
			if showConnector {
				// Connector is at displayIndent - 1. 
				// We use a simplified version for gollm's FlatNode.
				pos := indent - 1
				if pos >= 0 {
					childGutters = append(childGutters, GutterInfo{Position: pos, Show: !isLast})
				}
			}
			
			walk(child, childIndent, multipleChildren, childIsLast, childGutters)
		}
	}

	for i, root := range roots {
		indent := 0
		if multipleRoots {
			indent = 1
		}
		
		var initialGutters []GutterInfo
		if multipleRoots && i < len(roots)-1 {
			initialGutters = []GutterInfo{{Position: 0, Show: true}}
		}

		walk(root, indent, multipleRoots, i == len(roots)-1, initialGutters)
	}

	return result
}


// RenderTree returns a text tree using Unicode box-drawing characters.
func RenderTree(roots []*TreeNode, currentID string) string {
	var sb strings.Builder
	var render func(nodes []*TreeNode, prefix string, last bool)
	render = func(nodes []*TreeNode, prefix string, _ bool) {
		for i, n := range nodes {
			isLast := i == len(nodes)-1
			connector := "├── "
			childPrefix := prefix + "│   "
			if isLast {
				connector = "└── "
				childPrefix = prefix + "    "
			}
			marker := " "
			if n.ID == currentID {
				marker = "▶"
			}
			label := n.ID[:8]
			if n.Name != "" {
				label = n.Name
			}
			sb.WriteString(prefix)
			sb.WriteString(connector)
			sb.WriteString(marker)
			sb.WriteString(" ")
			sb.WriteString(label)
			sb.WriteString("  (")
			sb.WriteString(n.UpdatedAt.Format("Jan 02 15:04"))
			sb.WriteString(", ")
			sb.WriteString(formatMsgCount(n.MsgCount))
			sb.WriteString(")\n")
			if len(n.Children) > 0 {
				render(n.Children, childPrefix, isLast)
			}
		}
	}
	render(roots, "", true)
	return strings.TrimRight(sb.String(), "\n")
}

func formatMsgCount(n int) string {
	if n == 1 {
		return "1 msg"
	}
	s := ""
	for i := n; i > 0; i /= 10 {
		s = string(rune('0'+i%10)) + s
	}
	if n == 0 {
		return "0 msgs"
	}
	return s + " msgs"
}
