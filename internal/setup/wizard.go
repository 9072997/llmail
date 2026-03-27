package setup

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/jpennington/llmail/internal/config"
	"github.com/jpennington/llmail/internal/guard"
	"github.com/jpennington/llmail/internal/llm"
)

type WizardResult struct {
	Config *config.Config
	Path   string
}

func RunWizard() (*WizardResult, error) {
	fmt.Println("🔧 llmail Setup Wizard")
	fmt.Println("======================")
	if p := config.Profile(); p != config.DefaultProfile {
		fmt.Printf("  Profile: %s\n", p)
	}
	fmt.Println()

	path := config.DefaultConfigPath()
	existing, err := config.Load(path)
	if err == nil && len(existing.AccountNames()) > 0 {
		return runEditWizard(existing, path)
	}

	return runNewWizard()
}

func runNewWizard() (*WizardResult, error) {
	cfg := config.NewDefault()
	var allFolders = make(map[string][]string)

	// Account loop
	for {
		acc, folders, err := addAccount(nil)
		if err != nil {
			return nil, err
		}
		if acc == nil {
			break
		}

		cfg.SetAccount(acc.Name, acc.Config)
		allFolders[acc.Name] = folders

		var addAnother bool
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title("Add another account?").
					Value(&addAnother),
			),
		)
		if err := form.Run(); err != nil {
			return nil, err
		}
		if !addAnother {
			break
		}
	}

	if len(cfg.AccountNames()) == 0 {
		return nil, fmt.Errorf("no accounts configured")
	}

	// Index configuration
	if err := configureIndex(cfg, allFolders); err != nil {
		return nil, err
	}

	// LLM configuration
	if err := configureLLM(cfg); err != nil {
		return nil, err
	}

	// Guard configuration
	if err := configureGuard(cfg); err != nil {
		return nil, err
	}

	// Image configuration
	if err := configureImage(cfg); err != nil {
		return nil, err
	}

	// Save
	path := config.DefaultConfigPath()
	if err := cfg.Save(path); err != nil {
		return nil, fmt.Errorf("saving config: %w", err)
	}

	printSummary(cfg, path)

	return &WizardResult{Config: cfg, Path: path}, nil
}

func runEditWizard(cfg *config.Config, path string) (*WizardResult, error) {
	fmt.Println("  Existing configuration found.")
	fmt.Println()

	for {
		var action string
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title("What would you like to do?").
					Options(
						huh.NewOption("Edit an account", "edit_account"),
						huh.NewOption("Add a new account", "add_account"),
						huh.NewOption("Remove an account", "remove_account"),
						huh.NewOption("Configure index settings", "index"),
						huh.NewOption("Configure LLM settings", "llm"),
						huh.NewOption("Configure prompt injection guard", "guard"),
						huh.NewOption("Configure image settings", "image"),
						huh.NewOption("Start fresh (discard current config)", "fresh"),
						huh.NewOption("Done - save and exit", "done"),
					).
					Value(&action),
			),
		)

		if err := form.Run(); err != nil {
			return nil, err
		}

		switch action {
		case "edit_account":
			if err := editExistingAccount(cfg); err != nil {
				return nil, err
			}
		case "add_account":
			acc, _, err := addAccount(nil)
			if err != nil {
				return nil, err
			}
			if acc != nil {
				cfg.SetAccount(acc.Name, acc.Config)
			}
		case "remove_account":
			if err := removeAccount(cfg); err != nil {
				return nil, err
			}
		case "index":
			if err := configureIndex(cfg, nil); err != nil {
				return nil, err
			}
		case "llm":
			if err := configureLLM(cfg); err != nil {
				return nil, err
			}
		case "guard":
			if err := configureGuard(cfg); err != nil {
				return nil, err
			}
		case "image":
			if err := configureImage(cfg); err != nil {
				return nil, err
			}
		case "fresh":
			return runNewWizard()
		case "done":
			if len(cfg.AccountNames()) == 0 {
				return nil, fmt.Errorf("no accounts configured")
			}

			if err := cfg.Save(path); err != nil {
				return nil, fmt.Errorf("saving config: %w", err)
			}

			printSummary(cfg, path)
			return &WizardResult{Config: cfg, Path: path}, nil
		}
	}
}

func editExistingAccount(cfg *config.Config) error {
	names := cfg.AccountNames()
	if len(names) == 0 {
		fmt.Println("  No accounts to edit.")
		return nil
	}

	options := make([]huh.Option[string], len(names))
	for i, name := range names {
		acc, _ := cfg.GetAccount(name)
		label := fmt.Sprintf("%s (%s@%s)", name, acc.Username, acc.Host)
		options[i] = huh.NewOption(label, name)
	}

	var selected string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select account to edit").
				Options(options...).
				Value(&selected),
		),
	)
	if err := form.Run(); err != nil {
		return err
	}

	existing, _ := cfg.GetAccount(selected)
	acc, _, err := addAccount(&existingAccount{
		name:   selected,
		config: existing,
	})
	if err != nil {
		return err
	}
	if acc != nil {
		// If the name changed, remove the old one
		if acc.Name != selected {
			cfg.DeleteAccount(selected)
		}
		cfg.SetAccount(acc.Name, acc.Config)
	}
	return nil
}

type existingAccount struct {
	name   string
	config config.AccountConfig
}

func removeAccount(cfg *config.Config) error {
	names := cfg.AccountNames()
	if len(names) == 0 {
		fmt.Println("  No accounts to remove.")
		return nil
	}

	options := make([]huh.Option[string], len(names))
	for i, name := range names {
		acc, _ := cfg.GetAccount(name)
		label := fmt.Sprintf("%s (%s@%s)", name, acc.Username, acc.Host)
		options[i] = huh.NewOption(label, name)
	}

	var selected string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select account to remove").
				Options(options...).
				Value(&selected),
		),
	)
	if err := form.Run(); err != nil {
		return err
	}

	var confirm bool
	form = huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(fmt.Sprintf("Remove account '%s'?", selected)).
				Affirmative("Yes, remove").
				Negative("Cancel").
				Value(&confirm),
		),
	)
	if err := form.Run(); err != nil {
		return err
	}
	if confirm {
		cfg.DeleteAccount(selected)
		fmt.Printf("  Account '%s' removed.\n", selected)
	}
	return nil
}

type accountResult struct {
	Name   string
	Config config.AccountConfig
}

func addAccount(existing *existingAccount) (*accountResult, []string, error) {
	var (
		name     string
		host     string
		portStr  string
		useTLS   bool
		username string
		password string
	)

	// Pre-populate from existing account
	if existing != nil {
		name = existing.name
		host = existing.config.Host
		portStr = strconv.Itoa(existing.config.Port)
		useTLS = existing.config.TLS
		username = existing.config.Username
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Account nickname").
				Description("A short name for this account (e.g., 'work', 'personal')").
				Value(&name),
			huh.NewInput().
				Title("IMAP host").
				Description("e.g., imap.gmail.com, imap.fastmail.com").
				Value(&host),
			huh.NewInput().
				Title("Port").
				Description("Usually 993 for TLS").
				Value(&portStr).
				Placeholder("993"),
			huh.NewConfirm().
				Title("Use TLS?").
				Value(&useTLS).
				Affirmative("Yes").
				Negative("No"),
			huh.NewInput().
				Title("Username / Email").
				Value(&username),
			huh.NewInput().
				Title("Password / App Password").
				Description(passwordDescription(existing)).
				EchoMode(huh.EchoModePassword).
				Value(&password),
		),
	)

	if err := form.Run(); err != nil {
		return nil, nil, err
	}

	if name == "" {
		return nil, nil, nil
	}

	port := 993
	if portStr != "" {
		p, err := strconv.Atoi(portStr)
		if err == nil {
			port = p
		}
	}

	if !useTLS && portStr == "" {
		port = 143
	}

	// If editing and password is blank, keep the existing credentials
	keepExistingPassword := existing != nil && password == ""

	if !keepExistingPassword {
		// Validate connection with the new password
		fmt.Printf("\nConnecting to %s:%d...\n", host, port)
		result, err := ValidateConnection(context.Background(), host, port, useTLS, username, password)
		if err != nil {
			return nil, nil, fmt.Errorf("validation error: %w", err)
		}
		if !result.Success {
			return nil, nil, fmt.Errorf("connection failed: %s", result.Error)
		}

		fmt.Printf("✓ Connected successfully!\n")
		fmt.Printf("  Folders found: %d\n", len(result.Folders))

		if result.Capabilities.GmailExtensions {
			fmt.Printf("  ★ Gmail extensions detected - Gmail-native search will be available\n")
		}
		if result.Capabilities.Sort {
			fmt.Printf("  ★ SORT capability detected\n")
		}
		if result.Capabilities.Thread {
			fmt.Printf("  ★ THREAD capability detected\n")
		}
		fmt.Println()

		// Store password
		passwordStorage := config.PasswordStorageKeyring
		var encryptedPassword string

		if config.KeyringAvailable() {
			_, err := config.StorePassword(name, password, config.PasswordStorageKeyring)
			if err != nil {
				fmt.Printf("  Warning: keyring storage failed (%v), using encrypted fallback\n", err)
				passwordStorage = config.PasswordStorageEncrypted
				encryptedPassword, _ = config.StorePassword(name, password, config.PasswordStorageEncrypted)
			}
		} else {
			fmt.Println("  Keyring not available, using encrypted password storage")
			passwordStorage = config.PasswordStorageEncrypted
			encryptedPassword, _ = config.StorePassword(name, password, config.PasswordStorageEncrypted)
		}

		acc := &accountResult{
			Name: name,
			Config: config.AccountConfig{
				Host:              host,
				Port:              port,
				TLS:               useTLS,
				Username:          username,
				PasswordStorage:   passwordStorage,
				EncryptedPassword: encryptedPassword,
				Capabilities: config.Capabilities{
					GmailExtensions: result.Capabilities.GmailExtensions,
					Sort:            result.Capabilities.Sort,
					Thread:          result.Capabilities.Thread,
				},
			},
		}

		return acc, result.Folders, nil
	}

	// Keeping existing password - still validate connection
	existingPassword, err := config.RetrievePassword(existing.name, existing.config.PasswordStorage, existing.config.EncryptedPassword)
	if err != nil {
		return nil, nil, fmt.Errorf("retrieving existing password: %w", err)
	}

	fmt.Printf("\nConnecting to %s:%d...\n", host, port)
	result, err := ValidateConnection(context.Background(), host, port, useTLS, username, existingPassword)
	if err != nil {
		return nil, nil, fmt.Errorf("validation error: %w", err)
	}
	if !result.Success {
		return nil, nil, fmt.Errorf("connection failed: %s", result.Error)
	}

	fmt.Printf("✓ Connected successfully!\n")
	fmt.Println()

	// If the account name changed, re-store the password under the new name
	passwordStorage := existing.config.PasswordStorage
	encryptedPassword := existing.config.EncryptedPassword
	if name != existing.name {
		passwordStorage = config.PasswordStorageKeyring
		if config.KeyringAvailable() {
			_, err := config.StorePassword(name, existingPassword, config.PasswordStorageKeyring)
			if err != nil {
				passwordStorage = config.PasswordStorageEncrypted
				encryptedPassword, _ = config.StorePassword(name, existingPassword, config.PasswordStorageEncrypted)
			}
		} else {
			passwordStorage = config.PasswordStorageEncrypted
			encryptedPassword, _ = config.StorePassword(name, existingPassword, config.PasswordStorageEncrypted)
		}
	}

	acc := &accountResult{
		Name: name,
		Config: config.AccountConfig{
			Host:              host,
			Port:              port,
			TLS:               useTLS,
			Username:          username,
			PasswordStorage:   passwordStorage,
			EncryptedPassword: encryptedPassword,
			Capabilities: config.Capabilities{
				GmailExtensions: result.Capabilities.GmailExtensions,
				Sort:            result.Capabilities.Sort,
				Thread:          result.Capabilities.Thread,
			},
		},
	}

	return acc, result.Folders, nil
}

func passwordDescription(existing *existingAccount) string {
	if existing != nil {
		return "Leave blank to keep existing password"
	}
	return ""
}

func configureIndex(cfg *config.Config, allFolders map[string][]string) error {
	indexMode := cfg.Index.Mode
	if indexMode == "" {
		indexMode = "none"
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Local message index for full-text search").
				Options(
					huh.NewOption("Disabled - no local index", "none"),
					huh.NewOption("All folders", "all"),
					huh.NewOption("Selected folders only", "selected"),
				).
				Value(&indexMode),
		),
	)

	if err := form.Run(); err != nil {
		return err
	}

	cfg.Index.Mode = indexMode
	if indexMode == "none" {
		cfg.Index.Enabled = false
		return nil
	}

	cfg.Index.Enabled = true

	if indexMode == "selected" && allFolders != nil {
		cfg.Index.Accounts = make(map[string]config.IndexAccountConfig)

		for _, accountName := range cfg.AccountNames() {
			folders := allFolders[accountName]
			if len(folders) == 0 {
				continue
			}

			options := make([]huh.Option[string], len(folders))
			for i, f := range folders {
				options[i] = huh.NewOption(f, f)
			}

			var selected []string
			form := huh.NewForm(
				huh.NewGroup(
					huh.NewMultiSelect[string]().
						Title(fmt.Sprintf("Select folders to index for '%s'", accountName)).
						Options(options...).
						Value(&selected),
				),
			)

			if err := form.Run(); err != nil {
				return err
			}

			if len(selected) > 0 {
				cfg.Index.Accounts[accountName] = config.IndexAccountConfig{
					Folders: selected,
				}
			}
		}
	}

	// Sync interval
	intervalStr := strconv.Itoa(cfg.Index.SyncIntervalMinutes)
	form = huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Sync interval (minutes)").
				Value(&intervalStr).
				Placeholder("15"),
		),
	)

	if err := form.Run(); err != nil {
		return err
	}

	if intervalStr != "" {
		if interval, err := strconv.Atoi(intervalStr); err == nil && interval > 0 {
			cfg.Index.SyncIntervalMinutes = interval
		}
	}

	return nil
}

func configureLLM(cfg *config.Config) error {
	var wantLLM bool
	if cfg.LLM.Provider != "" {
		wantLLM = true
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Configure an LLM for 'llmail chat'?").
				Description("This lets you chat with an AI that can manage your email").
				Value(&wantLLM),
		),
	)
	if err := form.Run(); err != nil {
		return err
	}
	if !wantLLM {
		cfg.LLM = config.LLMConfig{}
		return nil
	}

	provider := cfg.LLM.Provider
	if provider == "" {
		provider = "openai"
	}

	form = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("LLM provider").
				Options(
					huh.NewOption("OpenAI", "openai"),
					huh.NewOption("Anthropic", "anthropic"),
					huh.NewOption("OpenRouter", "openrouter"),
					huh.NewOption("Ollama (local)", "ollama"),
					huh.NewOption("Other OpenAI-compatible", "openai-compatible"),
				).
				Value(&provider),
		),
	)
	if err := form.Run(); err != nil {
		return err
	}

	// Defaults per provider
	defaultModel := ""
	defaultBaseURL := ""
	showBaseURL := false
	needsKey := true

	switch provider {
	case "openai":
		defaultModel = "gpt-4o"
	case "anthropic":
		defaultModel = "claude-sonnet-4-20250514"
	case "openrouter":
		defaultModel = "openai/gpt-4o"
		defaultBaseURL = "https://openrouter.ai/api/v1"
		showBaseURL = true
	case "ollama":
		defaultModel = "llama3"
		defaultBaseURL = "http://localhost:11434/v1"
		showBaseURL = true
		needsKey = false
	case "openai-compatible":
		defaultModel = ""
		showBaseURL = true
	}

	// Pre-populate model from existing config if same provider
	model := ""
	if cfg.LLM.Provider == provider && cfg.LLM.Model != "" {
		model = cfg.LLM.Model
	}

	form = huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Model name").
				Value(&model).
				Placeholder(defaultModel),
		),
	)
	if err := form.Run(); err != nil {
		return err
	}
	if model == "" {
		model = defaultModel
	}

	// Base URL (conditional)
	baseURL := ""
	if showBaseURL {
		if cfg.LLM.Provider == provider && cfg.LLM.BaseURL != "" {
			baseURL = cfg.LLM.BaseURL
		} else {
			baseURL = defaultBaseURL
		}
		form = huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Base URL").
					Value(&baseURL).
					Placeholder(defaultBaseURL),
			),
		)
		if err := form.Run(); err != nil {
			return err
		}
		if baseURL == "" {
			baseURL = defaultBaseURL
		}
	}

	// API key (conditional)
	var apiKeyStorage config.PasswordStorage
	var encryptedAPIKey string

	if needsKey {
		var apiKey string
		desc := "Stored securely via OS keyring (or encrypted fallback) - never saved in plaintext"
		if cfg.LLM.Provider == provider && cfg.LLM.APIKeyStorage != "" {
			desc = "Leave blank to keep existing API key"
		}

		form = huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("API key").
					Description(desc).
					EchoMode(huh.EchoModePassword).
					Value(&apiKey),
			),
		)
		if err := form.Run(); err != nil {
			return err
		}

		if apiKey != "" {
			apiKeyStorage = config.PasswordStorageKeyring
			if config.KeyringAvailable() {
				_, err := config.StoreAPIKey(provider, apiKey, config.PasswordStorageKeyring)
				if err != nil {
					fmt.Printf("  Warning: keyring storage failed (%v), using encrypted fallback\n", err)
					apiKeyStorage = config.PasswordStorageEncrypted
					encryptedAPIKey, _ = config.StoreAPIKey(provider, apiKey, config.PasswordStorageEncrypted)
				}
			} else {
				fmt.Println("  Keyring not available, using encrypted API key storage")
				apiKeyStorage = config.PasswordStorageEncrypted
				encryptedAPIKey, _ = config.StoreAPIKey(provider, apiKey, config.PasswordStorageEncrypted)
			}
		} else if cfg.LLM.Provider == provider {
			// Keep existing key
			apiKeyStorage = cfg.LLM.APIKeyStorage
			encryptedAPIKey = cfg.LLM.EncryptedAPIKey
		}
	}

	cfg.LLM = config.LLMConfig{
		Provider:        provider,
		Model:           model,
		BaseURL:         baseURL,
		APIKeyStorage:   apiKeyStorage,
		EncryptedAPIKey: encryptedAPIKey,
	}

	// Test the connection
	fmt.Printf("\nTesting connection to %s/%s...\n", provider, model)
	if err := testLLM(cfg.LLM); err != nil {
		fmt.Printf("  ✗ Test failed: %v\n", err)
		fmt.Println("  Configuration saved anyway — you can fix the issue and retry.")
	} else {
		fmt.Println("  ✓ LLM is working!")
	}
	fmt.Println()

	return nil
}

func testLLM(cfg config.LLMConfig) error {
	provider, err := llm.NewProvider(cfg)
	if err != nil {
		return fmt.Errorf("creating provider: %w", err)
	}

	msgs := []llm.Message{
		{Role: "user", Content: []llm.ContentPart{{Type: "text", Text: "Say hello in one word."}}},
	}

	var got string
	_, _, err = provider.ChatStream(context.Background(), msgs, nil, func(d llm.StreamDelta) {
		got += d.Text
	})
	if err != nil {
		return err
	}
	if got == "" {
		return fmt.Errorf("model returned empty response")
	}
	return nil
}

func configureGuard(cfg *config.Config) error {
	enabled := cfg.Guard.Enabled

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Enable prompt injection guard?").
				Description("Scans email content for prompt injection attacks using a local ML model (~271 MB download)").
				Value(&enabled),
		),
	)
	if err := form.Run(); err != nil {
		return err
	}

	cfg.Guard.Enabled = enabled
	if !enabled {
		return nil
	}

	// Set default threshold if not already set
	if cfg.Guard.Threshold == 0 {
		cfg.Guard.Threshold = 0.80
	}

	thresholdStr := fmt.Sprintf("%.0f", cfg.Guard.Threshold*100)
	form = huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Detection threshold (%)").
				Description("Content scoring above this threshold is blocked (default: 80)").
				Value(&thresholdStr).
				Placeholder("80"),
		),
	)
	if err := form.Run(); err != nil {
		return err
	}

	if thresholdStr != "" {
		if v, err := strconv.Atoi(thresholdStr); err == nil && v > 0 && v <= 100 {
			cfg.Guard.Threshold = float64(v) / 100
		}
	}

	// Check if model is already downloaded
	modelPath := cfg.Guard.ModelPath
	if modelPath == "" {
		modelPath = guard.DefaultModelPath()
	}

	if !guard.ModelReady(modelPath) {
		var download bool
		form = huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title("Download the guard model now?").
					Description(fmt.Sprintf("Model will be saved to %s", modelPath)).
					Value(&download),
			),
		)
		if err := form.Run(); err != nil {
			return err
		}

		if download {
			fmt.Println("  Downloading Prompt Guard 2 22M...")
			err := guard.DownloadModel(modelPath, func(file string) {
				fmt.Printf("    %s\n", file)
			})
			if err != nil {
				fmt.Printf("  Warning: download failed: %v\n", err)
				fmt.Println("  You can retry later with: llmail guard download")
			} else {
				fmt.Println("  Download complete.")
			}
		} else {
			fmt.Println("  Run 'llmail guard download' later to fetch the model.")
		}
	} else {
		fmt.Println("  Guard model already downloaded.")
	}

	fmt.Println()
	return nil
}

func configureImage(cfg *config.Config) error {
	enableLimit := cfg.Image.MaxSizeBytes > 0

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Limit image attachment size?").
				Description("Oversized images will be re-encoded as JPEG and shrunk to fit").
				Value(&enableLimit),
		),
	)
	if err := form.Run(); err != nil {
		return err
	}

	if !enableLimit {
		cfg.Image.MaxSizeBytes = 0
		return nil
	}

	maxKBStr := "5120"
	if cfg.Image.MaxSizeBytes > 0 {
		maxKBStr = strconv.Itoa(cfg.Image.MaxSizeBytes / 1024)
		if maxKBStr == "0" {
			maxKBStr = "1"
		}
	}

	form = huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Max image size (KB)").
				Description("Images larger than this will be re-encoded as JPEG at 50% quality and progressively shrunk").
				Value(&maxKBStr).
				Placeholder("5120"),
		),
	)
	if err := form.Run(); err != nil {
		return err
	}

	if maxKBStr != "" {
		if kb, err := strconv.Atoi(maxKBStr); err == nil && kb > 0 {
			cfg.Image.MaxSizeBytes = kb * 1024
		}
	}

	fmt.Println()
	return nil
}

func printSummary(cfg *config.Config, path string) {
	fmt.Println()
	fmt.Println("═══════════════════════════════════════")
	fmt.Println("  Configuration Summary")
	fmt.Println("═══════════════════════════════════════")
	fmt.Println()

	if p := config.Profile(); p != config.DefaultProfile {
		fmt.Printf("  Profile: %s\n\n", p)
	}

	for _, name := range cfg.AccountNames() {
		acc, _ := cfg.GetAccount(name)
		fmt.Printf("  Account: %s\n", name)
		fmt.Printf("    Host: %s:%d (TLS: %v)\n", acc.Host, acc.Port, acc.TLS)
		fmt.Printf("    User: %s\n", acc.Username)
		caps := []string{}
		if acc.Capabilities.GmailExtensions {
			caps = append(caps, "Gmail")
		}
		if acc.Capabilities.Sort {
			caps = append(caps, "SORT")
		}
		if acc.Capabilities.Thread {
			caps = append(caps, "THREAD")
		}
		if len(caps) > 0 {
			fmt.Printf("    Capabilities: %s\n", strings.Join(caps, ", "))
		}
		fmt.Println()
	}

	fmt.Printf("  Index: %s\n", cfg.Index.Mode)
	if cfg.Index.Enabled {
		fmt.Printf("  Sync interval: %d minutes\n", cfg.Index.SyncIntervalMinutes)
	}
	fmt.Println()
	fmt.Printf("  Config saved to: %s\n", path)
	fmt.Println()

	if cfg.Guard.Enabled {
		fmt.Printf("  Guard: enabled (threshold: %.0f%%)\n", cfg.Guard.Threshold*100)
	} else {
		fmt.Printf("  Guard: disabled\n")
	}

	if cfg.Image.MaxSizeBytes > 0 {
		fmt.Printf("  Image limit: %d KB\n", cfg.Image.MaxSizeBytes/1024)
	} else {
		fmt.Printf("  Image limit: none\n")
	}
	fmt.Println()

	if cfg.LLM.Provider != "" {
		fmt.Printf("  LLM: %s/%s\n", cfg.LLM.Provider, cfg.LLM.Model)
		if cfg.LLM.BaseURL != "" {
			fmt.Printf("  Base URL: %s\n", cfg.LLM.BaseURL)
		}
		if cfg.LLM.APIKeyStorage != "" {
			fmt.Printf("  API key: stored (%s)\n", cfg.LLM.APIKeyStorage)
		}
		fmt.Println()
	}

	// MCP client integration help
	fmt.Println("═══════════════════════════════════════")
	fmt.Println("  Usage")
	fmt.Println("═══════════════════════════════════════")
	fmt.Println()

	profileFlag := ""
	profileArg := ""
	if p := config.Profile(); p != config.DefaultProfile {
		profileFlag = fmt.Sprintf(" --profile %s", p)
		profileArg = fmt.Sprintf(`, "--profile", "%s"`, p)
	}

	if cfg.LLM.Provider != "" {
		fmt.Println("  Self-hosted chat:")
		fmt.Printf("    llmail%s chat\n", profileFlag)
		fmt.Println()
	}

	fmt.Println("  Claude Desktop integration:")
	fmt.Println("  Add this to your Claude Desktop config (claude_desktop_config.json):")
	fmt.Println()
	fmt.Println(`  {`)
	fmt.Println(`    "mcpServers": {`)
	fmt.Println(`      "llmail": {`)
	fmt.Println(`        "command": "llmail",`)
	fmt.Printf("        \"args\": [\"serve\"%s]\n", profileArg)
	fmt.Println(`      }`)
	fmt.Println(`    }`)
	fmt.Println(`  }`)
	fmt.Println()
}
