package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/matutetandil/mycel/internal/parser"
	"github.com/matutetandil/mycel/internal/plugin"
)

var pluginCmd = &cobra.Command{
	Use:   "plugin",
	Short: "Manage plugins",
	Long: `Manage Mycel plugins — install, list, remove, or update.

Plugins extend Mycel with custom connectors and functions via WASM.
They can be sourced from local directories or git repositories.

Examples:
  mycel plugin install                   # Install all plugins from config
  mycel plugin install salesforce        # Install a specific plugin
  mycel plugin list                      # List cached plugins
  mycel plugin remove salesforce         # Remove a cached plugin
  mycel plugin update                    # Re-resolve and update all versions`,
}

var pluginInstallCmd = &cobra.Command{
	Use:   "install [name]",
	Short: "Install plugins",
	Long: `Install plugins declared in your configuration.

Without arguments, installs all plugins. With a name, installs only that plugin.
Git-sourced plugins are cloned into mycel_plugins/ and locked in plugins.lock.

Examples:
  mycel plugin install                         # Install all
  mycel plugin install salesforce              # Install specific plugin
  mycel plugin install --config ./my-service   # From specific config dir`,
	RunE: runPluginInstall,
}

var pluginListCmd = &cobra.Command{
	Use:   "list",
	Short: "List plugins",
	Long: `List all cached plugins in mycel_plugins/ and their lock status.

Shows plugin name, source, version, and whether it's locked in plugins.lock.`,
	RunE: runPluginList,
}

var pluginRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove a cached plugin",
	Long: `Remove a plugin from the mycel_plugins/ cache.

Also removes the entry from plugins.lock.

Examples:
  mycel plugin remove salesforce`,
	Args: cobra.ExactArgs(1),
	RunE: runPluginRemove,
}

var pluginUpdateCmd = &cobra.Command{
	Use:   "update [name]",
	Short: "Update plugin versions",
	Long: `Re-resolve version constraints and update plugins.lock.

Ignores the current lock file and resolves fresh versions from git tags.
Downloads new versions if they differ from the cached ones.

Examples:
  mycel plugin update                    # Update all plugins
  mycel plugin update salesforce         # Update specific plugin`,
	RunE: runPluginUpdate,
}

func init() {
	pluginCmd.AddCommand(pluginInstallCmd)
	pluginCmd.AddCommand(pluginListCmd)
	pluginCmd.AddCommand(pluginRemoveCmd)
	pluginCmd.AddCommand(pluginUpdateCmd)
}

func runPluginInstall(cmd *cobra.Command, args []string) error {
	loadDotEnv()

	decls, err := parsePluginDeclarations()
	if err != nil {
		return err
	}

	if len(decls) == 0 {
		fmt.Println("No plugins declared in configuration.")
		return nil
	}

	// Filter by name if provided
	if len(args) > 0 {
		name := args[0]
		filtered := make([]*plugin.PluginDeclaration, 0)
		for _, d := range decls {
			if d.Name == name {
				filtered = append(filtered, d)
			}
		}
		if len(filtered) == 0 {
			return fmt.Errorf("plugin %q not found in configuration", name)
		}
		decls = filtered
	}

	logger := createLogger()
	reg := plugin.NewRegistryWithLogger(configDir, logger)

	fmt.Printf("Installing %d plugin(s)...\n", len(decls))
	if err := reg.LoadAll(context.Background(), decls); err != nil {
		return fmt.Errorf("install failed: %w", err)
	}

	fmt.Printf("\n✓ %d plugin(s) installed\n", len(decls))
	for _, d := range decls {
		version := d.Version
		if version == "" {
			version = "local"
		}
		fmt.Printf("  - %s (%s)\n", d.Name, version)
	}

	return nil
}

func runPluginList(cmd *cobra.Command, args []string) error {
	cache := plugin.NewCacheManager(configDir)

	// Read lock file
	lf, _ := plugin.ReadLockFile(configDir)

	// List cached plugins
	cached, err := cache.List()
	if err != nil {
		return fmt.Errorf("failed to list cached plugins: %w", err)
	}

	// Also read declared plugins for context
	decls, _ := parsePluginDeclarations()

	if len(cached) == 0 && len(decls) == 0 {
		fmt.Println("No plugins found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tSOURCE\tVERSION\tSTATUS")
	fmt.Fprintln(w, "----\t------\t-------\t------")

	// Show declared plugins
	seen := make(map[string]bool)
	for _, d := range decls {
		status := "declared"
		version := d.Version
		if version == "" {
			version = "-"
		}

		if lf != nil {
			if entry := lf.GetEntry(d.Name); entry != nil {
				version = entry.Version
				status = "locked"

				v, err := plugin.ParseVersion(entry.Version)
				if err == nil && cache.IsCached(d.Source, v) {
					status = "installed"
				}
			}
		}

		if !plugin.IsGitSource(d.Source) {
			status = "local"
			version = "-"
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", d.Name, d.Source, version, status)
		seen[d.Name] = true
	}

	// Show cached plugins not in declarations
	for _, c := range cached {
		name := c.Source
		if idx := strings.LastIndex(name, "/"); idx >= 0 {
			name = name[idx+1:]
		}
		if seen[name] {
			continue
		}
		fmt.Fprintf(w, "%s\t%s\t%s\tcached (orphan)\n", name, c.Source, c.Version.String())
	}

	w.Flush()
	return nil
}

func runPluginRemove(cmd *cobra.Command, args []string) error {
	name := args[0]
	cache := plugin.NewCacheManager(configDir)

	// Find the source from declarations or lock file
	source := ""
	decls, _ := parsePluginDeclarations()
	for _, d := range decls {
		if d.Name == name {
			source = d.Source
			break
		}
	}

	if source == "" {
		// Try lock file
		lf, _ := plugin.ReadLockFile(configDir)
		if lf != nil {
			if entry := lf.GetEntry(name); entry != nil {
				source = entry.Source
			}
		}
	}

	if source != "" {
		if err := cache.RemoveByName(source); err != nil {
			fmt.Printf("Cache: %v\n", err)
		} else {
			fmt.Printf("✓ Removed cached files for %s\n", name)
		}
	} else {
		fmt.Printf("Plugin %q not found in configuration or lock file\n", name)
	}

	// Remove from lock file
	lf, _ := plugin.ReadLockFile(configDir)
	if lf != nil {
		if lf.GetEntry(name) != nil {
			lf.RemoveEntry(name)
			if err := plugin.WriteLockFile(configDir, lf); err != nil {
				return fmt.Errorf("failed to update lock file: %w", err)
			}
			fmt.Printf("✓ Removed %s from plugins.lock\n", name)
		}
	}

	return nil
}

func runPluginUpdate(cmd *cobra.Command, args []string) error {
	loadDotEnv()

	decls, err := parsePluginDeclarations()
	if err != nil {
		return err
	}

	if len(decls) == 0 {
		fmt.Println("No plugins declared in configuration.")
		return nil
	}

	// Filter by name if provided
	if len(args) > 0 {
		name := args[0]
		filtered := make([]*plugin.PluginDeclaration, 0)
		for _, d := range decls {
			if d.Name == name {
				filtered = append(filtered, d)
			}
		}
		if len(filtered) == 0 {
			return fmt.Errorf("plugin %q not found in configuration", name)
		}
		decls = filtered
	}

	// Delete lock file to force fresh resolution
	os.Remove(lockFilePath())

	logger := createLogger()
	reg := plugin.NewRegistryWithLogger(configDir, logger)

	fmt.Printf("Updating %d plugin(s)...\n", len(decls))
	if err := reg.LoadAll(context.Background(), decls); err != nil {
		return fmt.Errorf("update failed: %w", err)
	}

	fmt.Printf("\n✓ %d plugin(s) updated\n", len(decls))

	// Show resolved versions
	lf, _ := plugin.ReadLockFile(configDir)
	if lf != nil {
		for _, d := range decls {
			if entry := lf.GetEntry(d.Name); entry != nil {
				fmt.Printf("  - %s → %s\n", d.Name, entry.Version)
			}
		}
	}

	return nil
}

// parsePluginDeclarations parses the config to extract plugin declarations.
func parsePluginDeclarations() ([]*plugin.PluginDeclaration, error) {
	p := parser.NewHCLParser()
	config, err := p.Parse(context.Background(), configDir)
	if err != nil {
		return nil, fmt.Errorf("failed to parse configuration: %w", err)
	}
	return config.Plugins, nil
}

func lockFilePath() string {
	return configDir + "/plugins.lock"
}
