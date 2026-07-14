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
	"pjsk/backend/internal/logsafe"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"
)

func main() {
	usernameFlag := flag.String("username", "", "administrator username")
	flag.Parse()

	username := strings.TrimSpace(*usernameFlag)
	if username == "" {
		log.Fatal("-username is required")
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	connectCtx, connectCancel := context.WithTimeout(context.Background(), 10*time.Second)
	pool, err := database.Connect(connectCtx, cfg.DatabaseURL)
	connectCancel()
	if err != nil {
		log.Fatalf("connect to database: %s", logsafe.Category(err))
	}
	defer pool.Close()

	checkCtx, checkCancel := context.WithTimeout(context.Background(), 5*time.Second)
	var exists bool
	checkErr := pool.QueryRow(checkCtx, `
		select exists(
			select 1 from admins where lower(btrim(username)) = lower($1)
		)
	`, username).Scan(&exists)
	checkCancel()
	if checkErr != nil {
		log.Fatalf("check administrator: %s", logsafe.Category(checkErr))
	}
	if exists {
		log.Fatalf("administrator %q already exists", username)
	}

	password, err := readPassword("Password: ")
	if err != nil {
		log.Fatal(err)
	}
	confirmation, err := readPassword("Confirm password: ")
	if err != nil {
		log.Fatal(err)
	}
	if password != confirmation {
		log.Fatal("passwords do not match")
	}
	if len(password) < 8 {
		log.Fatal("password must contain at least 8 characters")
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		log.Fatalf("hash password: %v", err)
	}
	createCtx, createCancel := context.WithTimeout(context.Background(), 5*time.Second)
	_, err = pool.Exec(createCtx, `
		insert into admins (username, password_hash, status)
		values ($1, $2, 'active')
	`, username, string(passwordHash))
	createCancel()
	if err != nil {
		log.Fatalf("create administrator: %s", logsafe.Category(err))
	}

	fmt.Printf("Administrator %q created.\n", username)
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
