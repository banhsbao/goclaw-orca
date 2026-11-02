package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

const (
	agentFilesProtectedEnv = "GOCLAW_AGENT_FILES_PROTECTED_AGENTS"
	agentFilesBackupEnv    = "GOCLAW_AGENT_FILES_BACKUP_DIR"
	agentFilesMaxPatchSize = 1500
)

var agentFilesPatchable = []string{"AGENTS.md", "CAPABILITIES.md"}

type agentFilePatchResult struct {
	Agent        string `json:"agent"`
	File         string `json:"file"`
	DryRun       bool   `json:"dry_run"`
	ProposalHash string `json:"proposal_hash"`
	OldSHA256    string `json:"old_sha256"`
	NewSHA256    string `json:"new_sha256"`
	RemovedChars int    `json:"removed_chars"`
	AddedChars   int    `json:"added_chars"`
	Backup       string `json:"backup,omitempty"`
	Verified     bool   `json:"verified"`
}

func agentFilesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent-files",
		Short: "Read and patch live agent prompt files",
	}
	cmd.AddCommand(agentFilesGetCmd())
	cmd.AddCommand(agentFilesPatchCmd())
	cmd.AddCommand(agentFilesApproveCmd())
	return cmd
}

func agentFilesGetCmd() *cobra.Command {
	var agent, file string
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Print one prompt file of an agent",
		RunE: func(cmd *cobra.Command, args []string) error {
			content, err := fetchAgentFile(agent, file)
			if err != nil {
				return err
			}
			fmt.Print(content)
			return nil
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "", "agent key")
	cmd.Flags().StringVar(&file, "file", "AGENTS.md", "file name")
	_ = cmd.MarkFlagRequired("agent")
	return cmd
}

func agentFilesPatchCmd() *cobra.Command {
	var agent, file, oldText, newText, approval string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "patch",
		Short: "Replace one exact occurrence of text in an agent prompt file",
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := runAgentFilePatch(agent, file, oldText, newText, approval, dryRun)
			if err != nil {
				return err
			}
			return printJSON(res)
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "", "agent key")
	cmd.Flags().StringVar(&file, "file", "AGENTS.md", "file name (AGENTS.md or CAPABILITIES.md)")
	cmd.Flags().StringVar(&oldText, "old", "", "exact text to replace; must occur exactly once")
	cmd.Flags().StringVar(&newText, "new", "", "replacement text")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "validate without writing")
	cmd.Flags().StringVar(&approval, "approval", "", "approval code from agent-files approve, required when approvals are enabled")
	_ = cmd.MarkFlagRequired("agent")
	_ = cmd.MarkFlagRequired("old")
	return cmd
}

func runAgentFilePatch(agent, file, oldText, newText, approval string, dryRun bool) (agentFilePatchResult, error) {
	res := agentFilePatchResult{Agent: agent, File: file, DryRun: dryRun, ProposalHash: proposalHash(agent, file, oldText, newText)}
	if err := checkAgentFilePatchAllowed(agent, file, os.Getenv(agentFilesProtectedEnv)); err != nil {
		return res, err
	}
	current, err := fetchAgentFile(agent, file)
	if err != nil {
		return res, err
	}
	updated, err := applyAgentFilePatch(current, oldText, newText)
	if err != nil {
		return res, err
	}
	res.OldSHA256 = shortSHA256(current)
	res.NewSHA256 = shortSHA256(updated)
	res.RemovedChars = len([]rune(oldText))
	res.AddedChars = len([]rune(newText))
	if dryRun {
		return res, nil
	}
	if err := checkPatchApproval(res.ProposalHash, approval, os.Getenv(agentFilesApprovalSecretEnv), dryRun); err != nil {
		return res, err
	}
	if dir := os.Getenv(agentFilesBackupEnv); dir != "" {
		if res.Backup, err = writeAgentFileBackup(dir, agent, file, current); err != nil {
			return res, err
		}
	}
	if err := storeAgentFile(agent, file, updated); err != nil {
		return res, err
	}
	stored, err := fetchAgentFile(agent, file)
	if err != nil {
		return res, fmt.Errorf("verify: %w", err)
	}
	if stored != updated {
		return res, fmt.Errorf("verify: stored content differs from patched content")
	}
	res.Verified = true
	return res, nil
}

func checkAgentFilePatchAllowed(agent, file, protected string) error {
	if !slices.Contains(agentFilesPatchable, file) {
		return fmt.Errorf("file %q cannot be patched; allowed: %s", file, strings.Join(agentFilesPatchable, ", "))
	}
	for _, p := range strings.Split(protected, ",") {
		if strings.TrimSpace(p) == agent {
			return fmt.Errorf("agent %q is protected from patching", agent)
		}
	}
	return nil
}

func applyAgentFilePatch(content, oldText, newText string) (string, error) {
	if strings.TrimSpace(oldText) == "" {
		return "", fmt.Errorf("--old must not be empty")
	}
	if oldText == newText {
		return "", fmt.Errorf("--old and --new are identical")
	}
	if n := len([]rune(oldText)); n > agentFilesMaxPatchSize {
		return "", fmt.Errorf("--old is %d chars; max %d per patch", n, agentFilesMaxPatchSize)
	}
	if n := len([]rune(newText)); n > agentFilesMaxPatchSize {
		return "", fmt.Errorf("--new is %d chars; max %d per patch", n, agentFilesMaxPatchSize)
	}
	switch n := strings.Count(content, oldText); n {
	case 0:
		return "", fmt.Errorf("--old text not found; fetch the file again and copy the exact text")
	case 1:
		return strings.Replace(content, oldText, newText, 1), nil
	default:
		return "", fmt.Errorf("--old text occurs %d times; include more surrounding text so it is unique", n)
	}
}

func fetchAgentFile(agent, file string) (string, error) {
	payload, err := agentFilesRPC(protocol.MethodAgentsFileGet, map[string]any{"agentId": agent, "name": file})
	if err != nil {
		return "", err
	}
	var resp struct {
		File struct {
			Content string `json:"content"`
			Missing bool   `json:"missing"`
		} `json:"file"`
	}
	if err := json.Unmarshal(payload, &resp); err != nil {
		return "", fmt.Errorf("decode %s: %w", protocol.MethodAgentsFileGet, err)
	}
	if resp.File.Missing {
		return "", fmt.Errorf("agent %q has no %s", agent, file)
	}
	return resp.File.Content, nil
}

func storeAgentFile(agent, file, content string) error {
	_, err := agentFilesRPC(protocol.MethodAgentsFileSet, map[string]any{"agentId": agent, "name": file, "content": content})
	return err
}

func agentFilesRPC(method string, params map[string]any) ([]byte, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	resp, err := gatewayRPC(method, raw)
	if err != nil {
		return nil, err
	}
	if !resp.OK {
		msg := "unknown error"
		if resp.Error != nil {
			msg = resp.Error.Message
		}
		return nil, fmt.Errorf("%s: %s", method, msg)
	}
	return json.Marshal(resp.Payload)
}

func writeAgentFileBackup(dir, agent, file, content string) (string, error) {
	path := filepath.Join(dir, agent, fmt.Sprintf("%s.%s", time.Now().UTC().Format("20060102T150405Z"), file))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("backup: %w", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return "", fmt.Errorf("backup: %w", err)
	}
	return path, nil
}

func shortSHA256(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:12]
}
