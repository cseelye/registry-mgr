package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/cseelye/registry-mgr/internal/config"
	"github.com/cseelye/registry-mgr/internal/models"
	"github.com/cseelye/registry-mgr/internal/registry"
	"github.com/spf13/cobra"
)

var (
	cfg         *config.Config
	client      *registry.Client
	configFile  string
	registryURL string
	username    string
	password    string
	credFile    string
)

func main() {
	root := &cobra.Command{
		Use:          "registry-cli",
		Short:        "Manage a private Docker registry",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			var err error
			cfg, err = config.Load(configFile)
			if err != nil {
				return err
			}
			// Layer 4: CLI flags override everything
			if registryURL != "" {
				cfg.RegistryURL = registryURL
			}
			if username != "" {
				cfg.Username = username
			}
			if password != "" {
				cfg.Password = password
			}
			if credFile != "" {
				if err := config.ApplyCredentialsFile(cfg, credFile); err != nil {
					return err
				}
			}
			if cfg.RegistryURL == "" {
				return fmt.Errorf("registry URL is required (--registry, REGISTRY_MGR_URL, or config file)")
			}
			client = registry.NewClient(cfg.RegistryURL, cfg.Username, cfg.Password)
			return nil
		},
	}

	root.PersistentFlags().StringVar(&configFile, "config", "", "path to config file")
	root.PersistentFlags().StringVar(&registryURL, "registry", "", "registry URL (e.g. http://localhost:5000)")
	root.PersistentFlags().StringVarP(&username, "username", "u", "", "registry username")
	root.PersistentFlags().StringVarP(&password, "password", "p", "", "registry password")
	root.PersistentFlags().StringVar(&credFile, "credentials-file", "", "path to credentials file (username:password)")

	root.AddCommand(listCmd(), inspectCmd(), deleteCmd())

	if err := root.Execute(); err != nil {
		if errors.Is(err, registry.ErrUnauthorized) {
			fmt.Fprintln(os.Stderr, "Error: authentication failed — check your credentials (--username/--password or --credentials-file)")
		} else {
			fmt.Fprintln(os.Stderr, "Error:", err)
		}
		os.Exit(1)
	}
}

func listCmd() *cobra.Command {
	var long bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all repositories and their tags",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			repos, err := client.ListRepositories(ctx)
			if err != nil {
				return err
			}
			if len(repos) == 0 {
				fmt.Println("No repositories found.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			if long {
				fmt.Fprintln(w, "REPOSITORY\tTAG\tSIZE\tARCH\tDIGEST\tLABELS")
				for _, repo := range repos {
					tags, err := client.ListTags(ctx, repo)
					if err != nil {
						return err
					}
					for _, tag := range tags {
						img, err := client.GetImageDetails(ctx, repo, tag)
						if err != nil {
							return err
						}
						labels := formatLabels(img.Labels)
						fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
							img.Repository, img.Tag, formatBytes(img.Size),
							img.Arch, img.Digest, labels)
					}
				}
			} else {
				fmt.Fprintln(w, "REPOSITORY\tTAGS")
				for _, repo := range repos {
					tags, err := client.ListTags(ctx, repo)
					if err != nil {
						return err
					}
					if len(tags) == 0 {
						continue
					}
					fmt.Fprintf(w, "%s\t%s\n", repo, strings.Join(tags, ", "))
				}
			}
			return w.Flush()
		},
	}

	cmd.Flags().BoolVarP(&long, "long", "l", false, "show detailed info (digest, size, arch, labels)")
	return cmd
}

func inspectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "inspect <repo:tag>",
		Short: "Show detailed information about an image",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, tag, err := parseRepoTag(args[0])
			if err != nil {
				return err
			}
			ctx := context.Background()
			img, err := client.GetImageDetails(ctx, repo, tag)
			if err != nil {
				return err
			}
			printImageDetails(img)
			return nil
		},
	}
}

func deleteCmd() *cobra.Command {
	var dryRun bool
	var force bool

	cmd := &cobra.Command{
		Use:   "delete <pattern>",
		Short: "Delete images matching a pattern",
		Long: `Delete images matching a pattern in the form repo:tag.

Use * as a wildcard in either part:
  repo:*      delete all tags in a repository
  *:tag       delete a specific tag from all repositories
  repo:tag*   delete all tags starting with "tag" in a repository
  *:*         delete everything`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoPart, tagPart, err := parseRepoTag(args[0])
			if err != nil {
				return err
			}
			ctx := context.Background()

			repos, err := client.ListRepositories(ctx)
			if err != nil {
				return err
			}

			var matches []models.Image
			for _, repo := range repos {
				if !matchGlob(repoPart, repo) {
					continue
				}
				tags, err := client.ListTags(ctx, repo)
				if err != nil {
					return err
				}
				for _, tag := range tags {
					if matchGlob(tagPart, tag) {
						matches = append(matches, models.Image{Repository: repo, Tag: tag})
					}
				}
			}

			if len(matches) == 0 {
				fmt.Println("No images match the pattern.")
				return nil
			}

			fmt.Printf("The following %d image(s) will be deleted:\n", len(matches))
			for _, m := range matches {
				fmt.Printf("  %s:%s\n", m.Repository, m.Tag)
			}

			if dryRun {
				fmt.Println("\n(dry run — nothing was deleted)")
				return nil
			}

			if !force {
				fmt.Printf("\nDelete %d image(s)? [y/N] ", len(matches))
				var answer string
				fmt.Scanln(&answer)
				if strings.ToLower(strings.TrimSpace(answer)) != "y" {
					fmt.Println("Aborted.")
					return nil
				}
			}

			var failed int
			for _, m := range matches {
				fmt.Printf("Deleting %s:%s ... ", m.Repository, m.Tag)
				if err := client.DeleteTag(ctx, m.Repository, m.Tag); err != nil {
					fmt.Printf("ERROR: %v\n", err)
					failed++
				} else {
					fmt.Println("ok")
				}
			}
			if failed > 0 {
				return fmt.Errorf("%d deletion(s) failed", failed)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be deleted without deleting")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "skip confirmation prompt")
	return cmd
}

func parseRepoTag(s string) (repo, tag string, err error) {
	idx := strings.LastIndex(s, ":")
	if idx < 0 {
		return "", "", fmt.Errorf("invalid format %q: expected repo:tag", s)
	}
	return s[:idx], s[idx+1:], nil
}

func matchGlob(pattern, value string) bool {
	if pattern == "*" {
		return true
	}
	regexStr := "^" + strings.ReplaceAll(regexp.QuoteMeta(pattern), `\*`, `.*`) + "$"
	matched, _ := regexp.MatchString(regexStr, value)
	return matched
}

func printImageDetails(img *models.Image) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "Repository:\t%s\n", img.Repository)
	fmt.Fprintf(w, "Tag:\t%s\n", img.Tag)
	fmt.Fprintf(w, "Digest:\t%s\n", img.Digest)
	fmt.Fprintf(w, "Size:\t%s\n", formatBytes(img.Size))
	fmt.Fprintf(w, "Created:\t%s\n", img.CreatedAt.Format(time.RFC3339))
	fmt.Fprintf(w, "OS:\t%s\n", img.OS)
	fmt.Fprintf(w, "Architecture:\t%s\n", img.Arch)
	if len(img.Labels) > 0 {
		fmt.Fprintln(w, "Labels:")
		for k, v := range img.Labels {
			fmt.Fprintf(w, "  %s:\t%s\n", k, v)
		}
	}
	w.Flush()
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

func formatLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return "—"
	}
	pairs := make([]string, 0, len(labels))
	for k, v := range labels {
		pairs = append(pairs, k+"="+v)
	}
	return strings.Join(pairs, " ")
}
