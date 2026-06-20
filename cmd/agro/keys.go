package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"agro/internal/config"
)

// runLogin implements `agro login`: prompt for a provider (default Gemini),
// read the API key with hidden input, and write the config (0600).
func runLogin() error {
	path, err := config.Path()
	if err != nil {
		return err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}

	in := bufio.NewReader(os.Stdin)
	fmt.Printf("Provider [%s]: ", config.DefaultProvider)
	line, _ := in.ReadString('\n')
	prov := strings.ToLower(strings.TrimSpace(line))
	if prov == "" {
		prov = config.DefaultProvider
	}

	p, known := config.KnownProviders[prov]
	if !known {
		// Unknown provider: ask for the endpoint + model.
		fmt.Print("Base URL: ")
		bl, _ := in.ReadString('\n')
		p.BaseURL = strings.TrimSpace(bl)
		fmt.Print("Model: ")
		ml, _ := in.ReadString('\n')
		p.Model = strings.TrimSpace(ml)
	}

	key, err := readSecret(in, "API key (hidden): ")
	if err != nil {
		return err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("no key entered")
	}

	// Live validation is stubbed for now (see validateKey).
	if err := validateKey(p, key); err != nil {
		return fmt.Errorf("key validation failed: %w", err)
	}
	p.Key = key

	cfg.Providers[prov] = p
	cfg.Active = prov
	if err := config.Save(path, cfg); err != nil {
		return err
	}
	fmt.Printf("Saved provider %q and set it active.\nConfig: %s\n", prov, path)
	return nil
}

// validateKey is a stub: live key validation is intentionally skipped for now.
func validateKey(_ config.Provider, _ string) error { return nil }

// runKeys implements `agro keys <list|use|rm> [provider]`.
func runKeys(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: agro keys <list|use|rm> [provider]")
	}
	path, err := config.Path()
	if err != nil {
		return err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}

	switch args[0] {
	case "list":
		return keysList(cfg)
	case "use":
		if len(args) < 2 {
			return fmt.Errorf("usage: agro keys use <provider>")
		}
		return keysUse(path, cfg, strings.ToLower(args[1]))
	case "rm":
		if len(args) < 2 {
			return fmt.Errorf("usage: agro keys rm <provider>")
		}
		return keysRm(path, cfg, strings.ToLower(args[1]))
	default:
		return fmt.Errorf("unknown keys subcommand %q (want list|use|rm)", args[0])
	}
}

func keysList(cfg *config.Config) error {
	if len(cfg.Providers) == 0 {
		fmt.Println("no providers configured; run `agro login`")
		return nil
	}
	names := make([]string, 0, len(cfg.Providers))
	for n := range cfg.Providers {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		p := cfg.Providers[n]
		marker := "  "
		if n == cfg.Active {
			marker = "* "
		}
		fmt.Printf("%s%-11s %-55s %-24s key=%s\n", marker, n, p.BaseURL, p.Model, config.Mask(p.Key))
	}
	return nil
}

func keysUse(path string, cfg *config.Config, name string) error {
	if _, ok := cfg.Providers[name]; !ok {
		return fmt.Errorf("no such provider %q; run `agro login` first", name)
	}
	cfg.Active = name
	if err := config.Save(path, cfg); err != nil {
		return err
	}
	fmt.Printf("active provider: %s\n", name)
	return nil
}

func keysRm(path string, cfg *config.Config, name string) error {
	if _, ok := cfg.Providers[name]; !ok {
		return fmt.Errorf("no such provider %q", name)
	}
	delete(cfg.Providers, name)
	if cfg.Active == name {
		cfg.Active = ""
	}
	if err := config.Save(path, cfg); err != nil {
		return err
	}
	fmt.Printf("removed provider: %s\n", name)
	return nil
}

// readSecret prompts and reads a line without echoing it (via stty on Unix).
// If echo can't be disabled (no TTY), it falls back to visible input. It reads
// from the shared reader so it doesn't fight the caller over stdin buffering.
func readSecret(in *bufio.Reader, prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	restore := disableEcho()
	defer restore()
	line, err := in.ReadString('\n')
	fmt.Fprintln(os.Stderr)
	return line, err
}

func disableEcho() func() {
	cmd := exec.Command("stty", "-echo")
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return func() {} // no TTY / no stty: input stays visible
	}
	return func() {
		c := exec.Command("stty", "echo")
		c.Stdin = os.Stdin
		_ = c.Run()
	}
}
