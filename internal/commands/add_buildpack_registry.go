package commands

import (
	"strings"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/buildpacks/pack/internal/slices"
	"github.com/buildpacks/pack/internal/stringset"
	"github.com/buildpacks/pack/internal/style"
	"github.com/buildpacks/pack/registry"

	"github.com/buildpacks/pack/internal/config"
	"github.com/buildpacks/pack/logging"
)

func AddBuildpackRegistry(logger logging.Logger, cfg config.Config, cfgPath string) *cobra.Command {
	var (
		setDefault   bool
		registryType string
	)

	cmd := &cobra.Command{
		Use:   "add-buildpack-registry <name> <url>",
		Args:  cobra.ExactArgs(2),
		Short: "Adds a new buildpack registry to your pack config file",
		RunE: logError(logger, func(cmd *cobra.Command, args []string) error {
			newRegistry := config.Registry{
				Name: args[0],
				URL:  args[1],
				Type: registryType,
			}

			if newRegistry.Name == config.OfficialRegistryName {
				return errors.Errorf("%s is a reserved registry name, please provide a different name",
					style.Symbol(config.OfficialRegistryName))
			}

			err := addRegistry(newRegistry, setDefault, cfg, cfgPath)
			if err != nil {
				return err
			}

			logger.Infof("Successfully added %s to buildpack registries", style.Symbol(newRegistry.Name))

			return nil
		}),
	}
	cmd.Example = "pack add-buildpack-registry my-registry https://github.com/buildpacks/my-buildpack"
	cmd.Flags().BoolVar(&setDefault, "default", false, "Set this buildpack registry as the default")
	cmd.Flags().StringVar(&registryType, "type", "github", "Type of buildpack registry [git|github]")
	AddHelpFlag(cmd, "add-buildpack-registry")

	return cmd
}

func addRegistry(newRegistry config.Registry, setDefault bool, cfg config.Config, cfgPath string) error {
	if _, ok := stringset.FromSlice(registry.Types)[newRegistry.Type]; !ok {
		return errors.Errorf(
			"%s is not a valid type. Supported types are: %s.",
			style.Symbol(newRegistry.Type),
			strings.Join(slices.MapString(registry.Types, style.Symbol), ", "))
	}

	for _, r := range cfg.Registries {
		if r.Name == newRegistry.Name {
			return errors.Errorf(
				"Buildpack registry %s already exists.",
				style.Symbol(newRegistry.Name))
		}
	}

	if setDefault {
		cfg.DefaultRegistryName = newRegistry.Name
	}
	cfg.Registries = append(cfg.Registries, newRegistry)
	return config.Write(cfg, cfgPath)
}
