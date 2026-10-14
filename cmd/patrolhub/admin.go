package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/term"

	"github.com/akbarbinaji/patrolhub/internal/config"
	"github.com/akbarbinaji/patrolhub/internal/service"
)

// createAdmin implements `patrolhub create-admin <username>`. The password is read
// twice without echo on a terminal, or as two lines when stdin is piped.
func createAdmin(cfg config.Config, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: patrolhub create-admin <username>")
	}
	password, err := readPasswordTwice()
	if err != nil {
		return err
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	u, err := service.NewAdminService(pool).Create(ctx, args[0], password)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Admin %q dibuat (id %d).\n", u.Username, u.ID)
	return nil
}

func readPasswordTwice() (string, error) {
	fd := int(os.Stdin.Fd())
	var read func(prompt string) (string, error)
	if term.IsTerminal(fd) {
		read = func(prompt string) (string, error) {
			fmt.Fprint(os.Stderr, prompt)
			b, err := term.ReadPassword(fd)
			fmt.Fprintln(os.Stderr)
			return string(b), err
		}
	} else {
		sc := bufio.NewScanner(os.Stdin)
		read = func(string) (string, error) {
			if !sc.Scan() {
				return "", errors.New("expected the password twice on stdin")
			}
			return strings.TrimRight(sc.Text(), "\r"), nil
		}
	}

	first, err := read("Password: ")
	if err != nil {
		return "", err
	}
	second, err := read("Ulangi password: ")
	if err != nil {
		return "", err
	}
	if first != second {
		return "", errors.New("passwords do not match")
	}
	return first, nil
}
