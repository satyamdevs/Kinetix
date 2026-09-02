package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"orca/gateway/internal/keys"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "orca-keys:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("orca-keys", flag.ExitOnError)
	dsn := fs.String("dsn", envOr("POSTGRES_DSN", ""), "Postgres DSN (POSTGRES_DSN)")
	name := fs.String("name", "", "key name")
	owner := fs.String("owner", "", "key owner / team")
	rpm := fs.Int64("rpm", 0, "requests-per-minute cap (0 = unlimited)")
	tpm := fs.Int64("tpm", 0, "tokens-per-month cap (0 = unlimited)")
	ttl := fs.Duration("ttl", 0, "key expiry (e.g. 720h); 0 = never")
	fs.Parse(args)

	if *dsn == "" {
		return errors.New("no DSN: pass -dsn or set POSTGRES_DSN")
	}

	ctx := context.Background()
	keeper, err := openKeeper(ctx, *dsn)
	if err != nil {
		return err
	}
	defer keeper.Close()

	switch cmd := fs.Arg(0); cmd {
	case "create":
		raw, k, err := keys.Generate(*name, *owner,
			keys.Limit{RequestsPerMinute: *rpm, TokensPerMonth: *tpm}, *ttl)
		if err != nil {
			return err
		}
		if err := keeper.Create(ctx, k); err != nil {
			return err
		}
		fmt.Printf("id:      %s\n", k.ID)
		fmt.Printf("key:     %s\n", raw)
		fmt.Printf("prefix:  %s\n", k.Prefix)
		fmt.Printf("owner:   %s\n", k.Owner)
		fmt.Printf("rpm:     %d\n", k.Limit.RequestsPerMinute)
		fmt.Printf("tpm:     %d\n", k.Limit.TokensPerMonth)
		fmt.Println("note: the full key above is shown once; store it securely.")
		return nil

	case "list":
		all, err := keeper.List(ctx)
		if err != nil {
			return err
		}
		if len(all) == 0 {
			fmt.Println("no api keys")
			return nil
		}
		fmt.Printf("%-22s  %-8s  %-16s  %9s  %9s  %s\n",
			"ID", "STATE", "PREFIX", "RPM", "TPM", "OWNER")
		for _, k := range all {
			state := "enabled"
			if !k.Enabled {
				state = "disabled"
			}
			fmt.Printf("%-22s  %-8s  %-16s  %9d  %9d  %s\n",
				k.ID, state, k.Prefix, k.Limit.RequestsPerMinute, k.Limit.TokensPerMonth, k.Owner)
		}
		return nil

	case "disable", "enable":
		id := fs.Arg(1)
		if id == "" {
			return errors.New("missing key id")
		}
		enabled := cmd == "enable"
		if err := keeper.SetEnabled(ctx, id, enabled); err != nil {
			return err
		}
		fmt.Printf("key %s now %s\n", id, cmd)
		return nil

	case "revoke":
		id := fs.Arg(1)
		if id == "" {
			return errors.New("missing key id")
		}
		if err := keeper.Delete(ctx, id); err != nil {
			return err
		}
		fmt.Printf("key %s revoked\n", id)
		return nil

	default:
		return errors.New("usage: <create|list|disable|enable|revoke> [id]")
	}
}

func openKeeper(ctx context.Context, dsn string) (keys.Keeper, error) {
	return keys.NewPostgres(ctx, dsn)
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
