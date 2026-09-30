package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/nbharathik/slurm-dashboard/internal/meta"
)

// jsonSchema is the version of every --json output format.
const jsonSchema = 1

func newVersionCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Show version, commit, build date and Go version",
		Example: `  sdash version
  sdash version --json`,
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			info := meta.Info()
			out := cmd.OutOrStdout()
			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(struct {
					Schema int `json:"schema"`
					meta.BuildInfo
				}{jsonSchema, info})
			}
			_, err := fmt.Fprintf(out, "%s %s\ncommit:   %s\nbuilt:    %s\ngo:       %s\nplatform: %s\n",
				info.Name, info.Version, info.Commit, info.Date, info.GoVersion, info.Platform)
			if err == nil && info.Build != "" {
				_, err = fmt.Fprintf(out, "build:    %s\n", info.Build)
			}
			return err
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}
