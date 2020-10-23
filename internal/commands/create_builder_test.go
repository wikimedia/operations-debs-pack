package commands_test

import (
	"bytes"
	"io/ioutil"
	"path/filepath"
	"testing"

	"github.com/buildpacks/pack"
	"github.com/buildpacks/pack/builder"
	pubcfg "github.com/buildpacks/pack/config"
	"github.com/buildpacks/pack/internal/dist"

	"github.com/golang/mock/gomock"
	"github.com/heroku/color"
	"github.com/sclevine/spec"
	"github.com/sclevine/spec/report"
	"github.com/spf13/cobra"

	"github.com/buildpacks/pack/internal/commands"
	"github.com/buildpacks/pack/internal/commands/testmocks"
	"github.com/buildpacks/pack/internal/config"
	ilogging "github.com/buildpacks/pack/internal/logging"
	"github.com/buildpacks/pack/logging"
	h "github.com/buildpacks/pack/testhelpers"
)

const validConfig = `
[[buildpacks]]
  id = "some.buildpack"

[[order]]
	[[order.group]]
		id = "some.buildpack"

`

var validConfigStruct = builder.Config{
	Buildpacks: []builder.BuildpackConfig{{BuildpackInfo: dist.BuildpackInfo{ID: "some.buildpack"}}},
	Order:      dist.Order{{Group: []dist.BuildpackRef{{BuildpackInfo: dist.BuildpackInfo{ID: "some.buildpack"}}}}},
}

func TestCreateBuilderCommand(t *testing.T) {
	color.Disable(true)
	defer color.Disable(false)
	spec.Run(t, "Commands", testCreateBuilderCommand, spec.Parallel(), spec.Report(report.Terminal{}))
}

func testCreateBuilderCommand(t *testing.T, when spec.G, it spec.S) {
	var (
		command           *cobra.Command
		logger            logging.Logger
		outBuf            bytes.Buffer
		mockController    *gomock.Controller
		mockClient        *testmocks.MockPackClient
		tmpDir            string
		builderConfigPath string
		cfg               config.Config
	)

	it.Before(func() {
		var err error
		tmpDir, err = ioutil.TempDir("", "create-builder-test")
		h.AssertNil(t, err)
		builderConfigPath = filepath.Join(tmpDir, "builder.toml")
		cfg = config.Config{}

		mockController = gomock.NewController(t)
		mockClient = testmocks.NewMockPackClient(mockController)
		logger = ilogging.NewLogWithWriters(&outBuf, &outBuf)
		command = commands.CreateBuilder(logger, cfg, mockClient)
	})

	it.After(func() {
		mockController.Finish()
	})

	when("#CreateBuilder", func() {
		when("both --publish and --no-pull flags are specified", func() {
			it("errors with a descriptive message", func() {
				command.SetArgs([]string{
					"some/builder",
					"--config", "some-config-path",
					"--publish",
					"--no-pull",
				})
				err := command.Execute()
				h.AssertNotNil(t, err)
				h.AssertError(t, err, "The --publish and --no-pull flags cannot be used together. The --publish flag requires the use of remote images.")
			})
		})

		when("both --publish and pull-policy=never flags are specified", func() {
			it("errors with a descriptive message", func() {
				command.SetArgs([]string{
					"some/builder",
					"--config", "some-config-path",
					"--publish",
					"--pull-policy",
					"never",
				})
				err := command.Execute()
				h.AssertNotNil(t, err)
				h.AssertError(t, err, "--publish and --pull-policy never cannot be used together. The --publish flag requires the use of remote images.")
			})
		})

		when("no-pull flag is specified", func() {
			it.Before(func() {
				h.AssertNil(t, ioutil.WriteFile(builderConfigPath, []byte(validConfig), 0666))
			})

			it("works and logs warning", func() {
				opts := pack.CreateBuilderOptions{
					BuilderName: "some/builder",
					Config:      validConfigStruct,
					PullPolicy:  pubcfg.PullNever,
				}
				mockClient.EXPECT().CreateBuilder(gomock.Any(), opts).Return(nil)

				command.SetArgs([]string{
					"some/builder",
					"--config", builderConfigPath,
					"--no-pull",
				})
				h.AssertNil(t, command.Execute())
				h.AssertContains(t, outBuf.String(), "Warning: Flag --no-pull has been deprecated")
			})

			it("disregards no-pull if used with pull-policy always and logs warning", func() {
				opts := pack.CreateBuilderOptions{
					BuilderName: "some/builder",
					Config:      validConfigStruct,
					PullPolicy:  pubcfg.PullAlways,
				}
				mockClient.EXPECT().CreateBuilder(gomock.Any(), opts).Return(nil)

				command.SetArgs([]string{
					"some/builder",
					"--config", builderConfigPath,
					"--no-pull",
					"--pull-policy",
					"always",
				})
				h.AssertNil(t, command.Execute())
				output := outBuf.String()
				h.AssertContains(t, output, "Warning: Flag --no-pull has been deprecated")
				h.AssertContains(t, output, "Warning: Flag --no-pull ignored in favor of --pull-policy")
			})
		})

		when("--pull-policy", func() {
			it("returns error for unknown policy", func() {
				command.SetArgs([]string{
					"some/builder",
					"--config", builderConfigPath,
					"--pull-policy", "unknown-policy",
				})
				h.AssertError(t, command.Execute(), "parsing pull policy")
			})
		})

		when("--buildpack-registry flag is specified but experimental isn't set in the config", func() {
			it("errors with a descriptive message", func() {
				command.SetArgs([]string{
					"some/builder",
					"--config", "some-config-path",
					"--buildpack-registry", "some-registry",
				})
				err := command.Execute()
				h.AssertNotNil(t, err)
				h.AssertError(t, err, "Support for buildpack registries is currently experimental.")
			})
		})

		when("warnings encountered in builder.toml", func() {
			it.Before(func() {
				h.AssertNil(t, ioutil.WriteFile(builderConfigPath, []byte(`
[[buildpacks]]
  id = "some.buildpack"
`), 0666))
			})

			it("logs the warnings", func() {
				mockClient.EXPECT().CreateBuilder(gomock.Any(), gomock.Any()).Return(nil)

				command.SetArgs([]string{
					"some/builder",
					"--config", builderConfigPath,
				})
				h.AssertNil(t, command.Execute())

				h.AssertContains(t, outBuf.String(), "Warning: builder configuration: empty 'order' definition")
			})
		})

		when("uses --builder-config", func() {
			it.Before(func() {
				h.AssertNil(t, ioutil.WriteFile(builderConfigPath, []byte(validConfig), 0666))
			})

			it("errors with a descriptive message", func() {
				command.SetArgs([]string{
					"some/builder",
					"--builder-config", builderConfigPath,
				})
				h.AssertError(t, command.Execute(), "unknown flag: --builder-config")
			})
		})

		when("no config provided", func() {
			it("errors with a descriptive message", func() {
				command.SetArgs([]string{
					"some/builder",
				})
				h.AssertError(t, command.Execute(), "Please provide a builder config path")
			})
		})
	})
}
