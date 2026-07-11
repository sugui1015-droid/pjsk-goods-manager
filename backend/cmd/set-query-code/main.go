package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"pjsk/backend/internal/config"
	"pjsk/backend/internal/database"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"
)

func main() {
	cnFlag := flag.String("cn", "", "CN to set query code for")
	flag.Parse()

	cn := normalizeCN(*cnFlag)
	if cn == "" {
		log.Fatal("-cn is required")
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	connectCtx, connectCancel := context.WithTimeout(context.Background(), 10*time.Second)
	pool, err := database.Connect(connectCtx, cfg.DatabaseURL)
	connectCancel()
	if err != nil {
		log.Fatalf("connect to database: %v", err)
	}
	defer pool.Close()

	var userID string
	checkCtx, checkCancel := context.WithTimeout(context.Background(), 5*time.Second)
	err = pool.QueryRow(checkCtx, `
		select id::text
		from users
		where lower(regexp_replace(btrim(cn_code), '\s+', ' ', 'g')) = lower($1)
	`, cn).Scan(&userID)
	checkCancel()
	if err != nil {
		log.Fatalf("find CN %q: %v", cn, err)
	}

	code, err := readPassword("Query code: ")
	if err != nil {
		log.Fatal(err)
	}
	confirmation, err := readPassword("Confirm query code: ")
	if err != nil {
		log.Fatal(err)
	}
	if code != confirmation {
		log.Fatal("query codes do not match")
	}
	if len([]rune(code)) < 4 {
		log.Fatal("query code must contain at least 4 characters")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(code), 12)
	if err != nil {
		log.Fatalf("hash query code: %v", err)
	}

	updateCtx, updateCancel := context.WithTimeout(context.Background(), 5*time.Second)
	_, err = pool.Exec(updateCtx, `
		update users
		set query_code_hash = $2, updated_at = now()
		where id = $1::uuid
	`, userID, string(hash))
	updateCancel()
	if err != nil {
		log.Fatalf("set query code: %v", err)
	}

	fmt.Printf("Query code for CN %q has been set.\n", cn)
}

func normalizeCN(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func readPassword(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	value, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	return strings.TrimRight(string(value), "\r\n"), nil
}
