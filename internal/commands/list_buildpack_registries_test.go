package commands_test

import (
	"bytes"
	"testing"

	h "github.com/buildpacks/pack/testhelpers"

	"github.com/buildpacks/pack/internal/commands"

	"github.com/heroku/color"
	"github.com/sclevine/spec"
	"github.com/sclevine/spec/report"
	"github.com/spf13/cobra"

	"github.com/buildpacks/pack/internal/config"
	ilogging "github.com/buildpacks/pack/internal/logging"
	"github.com/buildpacks/pack/logging"
)

func TestListBuildpacksRegistries(t *testing.T) {
	color.Disable(true)
	defer color.Disable(false)

	spec.Run(t, "Commands", testListBuildpackRegistriesCommand, spec.Parallel(), spec.Report(report.Terminal{}))
}

func testListBuildpackRegistriesCommand(t *testing.T, when spec.G, it spec.S) {
	var (
		command *cobra.Command
		logger  logging.Logger
		outBuf  bytes.Buffer
		cfg     config.Config
	)

	it.Before(func() {
		logger = ilogging.NewLogWithWriters(&outBuf, &outBuf)
		cfg = config.Config{
			DefaultRegistryName: "private registry",
			Registries: []config.Registry{
				{
					Name: "public registry",
					Type: "github",
					URL:  "https://github.com/buildpacks/public-registry",
				},
				{
					Name: "private registry",
					Type: "github",
					URL:  "https://github.com/buildpacks/private-registry",
				},
				{
					Name: "personal registry",
					Type: "github",
					URL:  "https://github.com/buildpacks/personal-registry",
				},
			},
		}
		command = commands.ListBuildpackRegistries(logger, cfg)
	})

	when("#ListBuildpackRegistries", func() {
		it("should list all registries", func() {
			h.AssertNil(t, command.Execute())

			h.AssertContains(t, outBuf.String(), "public registry")
			h.AssertContains(t, outBuf.String(), "* private registry")
			h.AssertContains(t, outBuf.String(), "personal registry")
		})

		it("should list registries in verbose mode", func() {
			logger := ilogging.NewLogWithWriters(&outBuf, &outBuf, ilogging.WithVerbose())
			command = commands.ListBuildpackRegistries(logger, cfg)

			h.AssertNil(t, command.Execute())

			h.AssertContains(t, outBuf.String(), "public registry")
			h.AssertContains(t, outBuf.String(), "https://github.com/buildpacks/public-registry")

			h.AssertContains(t, outBuf.String(), "* private registry")
			h.AssertContains(t, outBuf.String(), "https://github.com/buildpacks/private-registry")

			h.AssertContains(t, outBuf.String(), "personal registry")
			h.AssertContains(t, outBuf.String(), "https://github.com/buildpacks/personal-registry")

			h.AssertContains(t, outBuf.String(), "official")
			h.AssertContains(t, outBuf.String(), "https://github.com/buildpacks/registry-index")
		})

		it("should indicate official as the default registry by default", func() {
			logger := ilogging.NewLogWithWriters(&outBuf, &outBuf)
			cfg.DefaultRegistryName = ""

			command = commands.ListBuildpackRegistries(logger, cfg)

			h.AssertNil(t, command.Execute())

			h.AssertContains(t, outBuf.String(), "* official")
			h.AssertContains(t, outBuf.String(), "public registry")
			h.AssertContains(t, outBuf.String(), "private registry")
			h.AssertContains(t, outBuf.String(), "personal registry")
		})

		it("should use official when no registries are defined", func() {
			logger := ilogging.NewLogWithWriters(&outBuf, &outBuf)
			cfg.DefaultRegistryName = ""

			command = commands.ListBuildpackRegistries(logger, config.Config{})

			h.AssertNil(t, command.Execute())

			h.AssertContains(t, outBuf.String(), "* official")
		})
	})
}
