// Package admincli implements the server-side owner maintenance commands:
//
//	pjsk-backend promote-owner        --env-file <path> --username <name>
//	pjsk-backend reset-owner-password --env-file <path>
//
// Both are interactive, terminal-only commands intended for the server's SSH
// management environment. They never accept a password on the command line,
// never echo password input, and never print a password to stdout or logs.
// Neither command runs database migrations; they refuse to act on a database
// that has not applied migration 0022.
package admincli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"

	"github.com/joho/godotenv"

	"pjsk/backend/internal/admin"
	"pjsk/backend/internal/config"
	"pjsk/backend/internal/database"
	"pjsk/backend/internal/logsafe"
)

const requiredMigration = "0022_admin_owner_security.sql"

// cliClientIP is the audit client_ip marker for server-local CLI actions.
const cliClientIP = "server-cli"

// Run dispatches a CLI subcommand and returns the process exit code.
func Run(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: pjsk-backend [promote-owner|reset-owner-password] --env-file <path> [--username <name>]")
		return 2
	}
	switch args[0] {
	case "promote-owner":
		return runWithStore(args[1:], true, promoteOwner)
	case "reset-owner-password":
		return runWithStore(args[1:], false, resetOwnerPassword)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", args[0])
		return 2
	}
}

type cliContext struct {
	store    *admin.PostgresStore
	username string
	stdin    *bufio.Reader
}

func runWithStore(args []string, wantsUsername bool, action func(context.Context, *cliContext) error) int {
	flags := flag.NewFlagSet("pjsk-backend-cli", flag.ContinueOnError)
	envFile := flags.String("env-file", "", "path to the backend environment file (e.g. /etc/pjsk/backend.env)")
	username := flags.String("username", "", "target admin username (promote-owner only)")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if wantsUsername && strings.TrimSpace(*username) == "" {
		fmt.Fprintln(os.Stderr, "--username is required")
		return 2
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprintln(os.Stderr, "this command requires an interactive terminal")
		return 1
	}
	if *envFile != "" {
		if err := godotenv.Load(*envFile); err != nil {
			fmt.Fprintf(os.Stderr, "load env file: %v\n", err)
			return 1
		}
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect: %s\n", logsafe.Category(err))
		return 1
	}
	defer pool.Close()

	store := admin.NewPostgresStore(pool)
	applied, err := store.SchemaMigrationApplied(ctx, requiredMigration)
	if err != nil {
		fmt.Fprintf(os.Stderr, "check migrations: %s\n", logsafe.Category(err))
		return 1
	}
	if !applied {
		fmt.Fprintf(os.Stderr, "refusing to run: migration %s is not applied on this database\n", requiredMigration)
		return 1
	}

	if err := action(ctx, &cliContext{store: store, username: strings.TrimSpace(*username), stdin: bufio.NewReader(os.Stdin)}); err != nil {
		fmt.Fprintf(os.Stderr, "failed: %s\n", logsafe.Category(err))
		return 1
	}
	return 0
}

// printAccount shows only non-sensitive columns.
func printAccount(account admin.Admin) {
	display := "-"
	if account.DisplayName != nil && *account.DisplayName != "" {
		display = *account.DisplayName
	}
	fmt.Printf("  id:           %s\n  username:     %s\n  display name: %s\n  role:         %s\n  status:       %s\n",
		account.ID, account.Username, display, account.Role, account.Status)
}

// confirmUsername requires the operator to retype the username verbatim.
func confirmUsername(c *cliContext, expected string) error {
	fmt.Printf("Type the username exactly to confirm: ")
	line, err := c.stdin.ReadString('\n')
	if err != nil {
		return err
	}
	if strings.TrimRight(line, "\r\n") != expected {
		return errors.New("confirmation did not match; nothing was changed")
	}
	return nil
}

// readNewPassword prompts twice with echo disabled and applies the shared
// password policy. The plaintext lives only in this process's memory.
func readNewPassword(username string) (string, error) {
	fmt.Print("New password (input hidden): ")
	first, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", err
	}
	fmt.Print("Repeat new password: ")
	second, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", err
	}
	if string(first) != string(second) {
		return "", errors.New("passwords did not match; nothing was changed")
	}
	if err := admin.ValidatePassword(string(first), username); err != nil {
		return "", err
	}
	return string(first), nil
}

func cliAuditEvent(eventType admin.AdminAuthEventType, account admin.Admin) admin.AdminAuthAuditEvent {
	id := account.ID
	return admin.AdminAuthAuditEvent{
		EventType:          eventType,
		OccurredAt:         time.Now(),
		AdminID:            &id,
		UsernameNormalized: strings.ToLower(strings.TrimSpace(account.Username)),
		ClientIP:           cliClientIP,
		Result:             admin.AdminAuthResultSuccess,
		ReasonCode:         admin.AdminAuthReasonNone,
	}
}

// promoteOwner bootstraps the single owner. It only ever succeeds while the
// database has zero owners; both the store transaction and the partial
// unique index enforce that independently.
func promoteOwner(ctx context.Context, c *cliContext) error {
	owners, err := c.store.CountOwners(ctx)
	if err != nil {
		return err
	}
	if owners != 0 {
		return errors.New("an owner already exists; promotion is only allowed when there is no owner")
	}
	account, err := c.store.FindByUsername(ctx, c.username)
	if err != nil {
		if errors.Is(err, admin.ErrNotFound) {
			return fmt.Errorf("no admin named %q", c.username)
		}
		return err
	}
	if account.Status != "active" {
		return fmt.Errorf("admin %q is not active and cannot become owner", account.Username)
	}

	fmt.Println("About to promote this admin to the single system owner:")
	printAccount(account)
	if err := confirmUsername(c, account.Username); err != nil {
		return err
	}
	if err := c.store.PromoteOwner(ctx, account.ID, cliAuditEvent(admin.AdminAuthEventOwnerPromoted, account)); err != nil {
		return err
	}
	fmt.Printf("Promoted %q to owner. The audit event has been recorded.\n", account.Username)
	return nil
}

// resetOwnerPassword is the emergency recovery of last resort. It works on
// the single owner account only, revokes every session, and never reveals
// the password anywhere.
func resetOwnerPassword(ctx context.Context, c *cliContext) error {
	account, err := c.store.FindOwner(ctx)
	if err != nil {
		if errors.Is(err, admin.ErrNotFound) {
			return errors.New("no owner exists; run promote-owner first")
		}
		return err
	}

	fmt.Println("About to reset the password of the system owner:")
	printAccount(account)
	if err := confirmUsername(c, account.Username); err != nil {
		return err
	}
	password, err := readNewPassword(account.Username)
	if err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if err := c.store.ResetOwnerPassword(ctx, account.ID, string(hash), cliAuditEvent(admin.AdminAuthEventOwnerCLIPasswordReset, account)); err != nil {
		return err
	}
	fmt.Println("Owner password has been reset. All owner sessions were revoked and the audit event recorded.")
	fmt.Println("The new password was not written anywhere; store it in your password manager now.")
	return nil
}
