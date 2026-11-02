package cmd

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
)

const (
	agentFilesApprovalSecretEnv = "GOCLAW_AGENT_FILES_APPROVAL_SECRET"
	agentFilesApprovalCodeLen   = 16
)

var proposalHashPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

func agentFilesApproveCmd() *cobra.Command {
	var hash string
	cmd := &cobra.Command{
		Use:   "approve",
		Short: "Issue the approval code for one proposed patch, identified by its proposal hash",
		RunE: func(cmd *cobra.Command, args []string) error {
			code, err := approvalCodeFor(hash, os.Getenv(agentFilesApprovalSecretEnv))
			if err != nil {
				return err
			}
			return printJSON(map[string]string{"proposal_hash": hash, "approval": code})
		},
	}
	cmd.Flags().StringVar(&hash, "hash", "", "proposal_hash printed by agent-files patch --dry-run")
	_ = cmd.MarkFlagRequired("hash")
	return cmd
}

func proposalHash(agent, file, oldText, newText string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{agent, file, oldText, newText}, "\x00")))
	return hex.EncodeToString(sum[:])[:16]
}

func approvalCodeFor(hash, secret string) (string, error) {
	if secret == "" {
		return "", fmt.Errorf("%s is not set; approvals are not enabled", agentFilesApprovalSecretEnv)
	}
	if !proposalHashPattern.MatchString(hash) {
		return "", fmt.Errorf("--hash must be the 16-character proposal_hash from a dry run")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(hash))
	return hex.EncodeToString(mac.Sum(nil))[:agentFilesApprovalCodeLen], nil
}

func checkPatchApproval(hash, approval, secret string, dryRun bool) error {
	if secret == "" || dryRun {
		return nil
	}
	if approval == "" {
		return fmt.Errorf("this patch needs founder approval: run with --dry-run, report proposal_hash %s, and apply only with the --approval code you receive", hash)
	}
	want, err := approvalCodeFor(hash, secret)
	if err != nil {
		return err
	}
	if !hmac.Equal([]byte(want), []byte(strings.TrimSpace(approval))) {
		return fmt.Errorf("--approval does not match this exact patch (proposal_hash %s); the agent, file, old and new text must be identical to the approved proposal", hash)
	}
	return nil
}
