package commands_test

import (
	"bytes"
	"errors"
	"regexp"
	"testing"

	"github.com/Masterminds/semver"
	"github.com/buildpacks/lifecycle/api"
	"github.com/golang/mock/gomock"
	"github.com/heroku/color"
	"github.com/sclevine/spec"
	"github.com/sclevine/spec/report"
	"github.com/spf13/cobra"

	"github.com/buildpacks/pack"
	"github.com/buildpacks/pack/internal/builder"
	"github.com/buildpacks/pack/internal/commands"
	"github.com/buildpacks/pack/internal/commands/testmocks"
	"github.com/buildpacks/pack/internal/config"
	"github.com/buildpacks/pack/internal/dist"
	ilogging "github.com/buildpacks/pack/internal/logging"
	"github.com/buildpacks/pack/logging"
	h "github.com/buildpacks/pack/testhelpers"
)

func TestInspectBuilderCommand(t *testing.T) {
	color.Disable(true)
	defer color.Disable(false)
	spec.Run(t, "Commands", testInspectBuilderCommand, spec.Parallel(), spec.Report(report.Terminal{}))
}

const inspectBuilderRemoteOutputSection = `
REMOTE:

Description: Some remote description

Created By:
  Name: Pack CLI
  Version: 1.2.3

Trusted: No

Stack:
  ID: test.stack.id

Lifecycle:
  Version: 6.7.8
  Buildpack APIs:
    Deprecated: (none)
    Supported: 1.2, 2.3
  Platform APIs:
    Deprecated: 0.1, 1.2
    Supported: 4.5

Run Images:
  first/local     (user-configured)
  second/local    (user-configured)
  some/run-image
  first/default
  second/default

Buildpacks:
  ID                     VERSION                        HOMEPAGE
  test.top.nested        test.top.nested.version        
  test.nested            test.nested.version            http://geocities.com/top-bp
  test.bp.one            test.bp.one.version            http://geocities.com/cool-bp
  test.bp.two            test.bp.two.version            
  test.bp.three          test.bp.three.version          

Detection Order:
 └ Group #1:
    ├ test.top.nested@test.top.nested.version    
    │  └ Group #1:
    │     ├ test.nested@test.nested.version    
    │     │  └ Group #1:
    │     │     └ test.bp.one@test.bp.one.version    (optional)
    │     └ test.bp.three@test.bp.three.version      (optional)
    └ test.bp.two                                    (optional)
`

const inspectBuilderLocalOutputSection = `
LOCAL:

Description: Some local description

Created By:
  Name: Pack CLI
  Version: 4.5.6

Trusted: No

Stack:
  ID: test.stack.id

Lifecycle:
  Version: 4.5.6
  Buildpack APIs:
    Deprecated: 4.5, 6.7
    Supported: 8.9, 10.11
  Platform APIs:
    Deprecated: (none)
    Supported: 7.8

Run Images:
  first/local     (user-configured)
  second/local    (user-configured)
  some/run-image
  first/local-default
  second/local-default

Buildpacks:
  ID                     VERSION                        HOMEPAGE
  test.top.nested        test.top.nested.version        
  test.nested            test.nested.version            http://geocities.com/top-bp
  test.bp.one            test.bp.one.version            http://geocities.com/cool-bp
  test.bp.two            test.bp.two.version            
  test.bp.three          test.bp.three.version          

Detection Order:
 └ Group #1:
    ├ test.top.nested@test.top.nested.version    
    │  └ Group #1:
    │     ├ test.nested@test.nested.version    
    │     │  └ Group #1:
    │     │     └ test.bp.one@test.bp.one.version    (optional)
    │     └ test.bp.three@test.bp.three.version      (optional)
    └ test.bp.two                                    (optional)
`

const stackLabelsSection = `
Stack:
  ID: test.stack.id
  Mixins:
    mixin1
    mixin2
    build:mixin3
    build:mixin4
`

const detectionOrderWithDepth = `Detection Order:
 └ Group #1:
    ├ test.top.nested@test.top.nested.version    
    │  └ Group #1:
    │     ├ test.nested@test.nested.version        
    │     └ test.bp.three@test.bp.three.version    (optional)
    └ test.bp.two                                  (optional)`

const detectionOrderWithCycle = `Detection Order:
 ├ Group #1:
 │  ├ test.top.nested@test.top.nested.version
 │  │  └ Group #1:
 │  │     └ test.nested@test.nested.version
 │  │        └ Group #1:
 │  │           └ test.top.nested@test.top.nested.version    [cyclic]
 │  └ test.bp.two                                            (optional)
 └ Group #2:
    └ test.nested@test.nested.version
       └ Group #1:
          └ test.top.nested@test.top.nested.version
             └ Group #1:
                └ test.nested@test.nested.version    [cyclic]
`
const selectDefaultBuilderOutput = `Please select a default builder with:

	pack set-default-builder <builder-image>`

func testInspectBuilderCommand(t *testing.T, when spec.G, it spec.S) {
	apiVersion, err := api.NewVersion("0.2")
	if err != nil {
		t.Fail()
	}

	var (
		command        *cobra.Command
		logger         logging.Logger
		outBuf         bytes.Buffer
		assert         = h.NewAssertionManager(t)
		mockController *gomock.Controller
		mockClient     *testmocks.MockPackClient
		cfg            config.Config
		buildpacks     = []dist.BuildpackInfo{
			{
				ID:      "test.top.nested",
				Version: "test.top.nested.version",
			},
			{
				ID:       "test.nested",
				Version:  "test.nested.version",
				Homepage: "http://geocities.com/top-bp",
			},
			{
				ID:       "test.bp.one",
				Version:  "test.bp.one.version",
				Homepage: "http://geocities.com/cool-bp",
			},
			{
				ID:      "test.bp.two",
				Version: "test.bp.two.version",
			},
			{
				ID:      "test.bp.three",
				Version: "test.bp.three.version",
			},
		}
		order = dist.Order{
			{
				Group: []dist.BuildpackRef{
					{
						BuildpackInfo: dist.BuildpackInfo{ID: "test.top.nested", Version: "test.top.nested.version"},
						Optional:      false,
					},
					{
						BuildpackInfo: dist.BuildpackInfo{ID: "test.bp.two"},
						Optional:      true,
					},
				},
			},
		}
		buildpackLayers = map[string]map[string]dist.BuildpackLayerInfo{
			"test.top.nested": {
				"test.top.nested.version": {
					API: apiVersion,
					Order: dist.Order{
						{
							Group: []dist.BuildpackRef{
								{
									BuildpackInfo: dist.BuildpackInfo{
										ID:      "test.nested",
										Version: "test.nested.version",
									},
									Optional: false,
								},
								{
									BuildpackInfo: dist.BuildpackInfo{
										ID:      "test.bp.three",
										Version: "test.bp.three.version",
									},
									Optional: true,
								},
							},
						},
					},
					LayerDiffID: "sha256:test.top.nested.sha256",
				},
			},
			"test.nested": {
				"test.nested.version": {
					API: apiVersion,
					Order: dist.Order{
						{
							Group: []dist.BuildpackRef{
								{
									BuildpackInfo: dist.BuildpackInfo{
										ID:      "test.bp.one",
										Version: "test.bp.one.version",
									},
									Optional: true,
								},
							},
						},
					},
					LayerDiffID: "sha256:test.nested.sha256",
					Homepage:    "http://geocities.com/top-bp",
				},
			},
			"test.bp.one": {
				"test.bp.one.version": {
					API: apiVersion,
					Stacks: []dist.Stack{
						{
							ID: "test.stack.id",
						},
					},
					LayerDiffID: "sha256:test.bp.one.sha256",
					Homepage:    "http://geocities.com/cool-bp",
				},
			},
			"test.bp.two": {
				"test.bp.two.version": {
					API: apiVersion,
					Stacks: []dist.Stack{
						{
							ID: "test.stack.id",
						},
					},
					LayerDiffID: "sha256:test.bp.two.sha256",
				},
			},
			"test.bp.three": {
				"test.bp.three.version": {
					API: apiVersion,
					Stacks: []dist.Stack{
						{
							ID: "test.stack.id",
						},
					},
					LayerDiffID: "sha256:test.bp.three.sha256",
				},
			},
		}

		remoteInfo = &pack.BuilderInfo{
			Description:     "Some remote description",
			Stack:           "test.stack.id",
			Mixins:          []string{"mixin1", "mixin2", "build:mixin3", "build:mixin4"},
			RunImage:        "some/run-image",
			RunImageMirrors: []string{"first/default", "second/default"},
			Buildpacks:      buildpacks,
			Order:           order,
			BuildpackLayers: buildpackLayers,
			Lifecycle: builder.LifecycleDescriptor{
				Info: builder.LifecycleInfo{
					Version: &builder.Version{
						Version: *semver.MustParse("6.7.8"),
					},
				},
				APIs: builder.LifecycleAPIs{
					Buildpack: builder.APIVersions{
						Deprecated: nil,
						Supported:  builder.APISet{api.MustParse("1.2"), api.MustParse("2.3")},
					},
					Platform: builder.APIVersions{
						Deprecated: builder.APISet{api.MustParse("0.1"), api.MustParse("1.2")},
						Supported:  builder.APISet{api.MustParse("4.5")},
					},
				},
			},
			CreatedBy: builder.CreatorMetadata{
				Name:    "Pack CLI",
				Version: "1.2.3",
			},
		}
		localInfo = &pack.BuilderInfo{
			Description:     "Some local description",
			Stack:           "test.stack.id",
			Mixins:          []string{"mixin1", "mixin2", "build:mixin3", "build:mixin4"},
			RunImage:        "some/run-image",
			RunImageMirrors: []string{"first/local-default", "second/local-default"},
			Buildpacks:      buildpacks,
			Order:           order,
			BuildpackLayers: buildpackLayers,
			Lifecycle: builder.LifecycleDescriptor{
				Info: builder.LifecycleInfo{
					Version: &builder.Version{
						Version: *semver.MustParse("4.5.6"),
					},
				},
				APIs: builder.LifecycleAPIs{
					Buildpack: builder.APIVersions{
						Deprecated: builder.APISet{api.MustParse("4.5"), api.MustParse("6.7")},
						Supported:  builder.APISet{api.MustParse("8.9"), api.MustParse("10.11")},
					},
					Platform: builder.APIVersions{
						Deprecated: nil,
						Supported:  builder.APISet{api.MustParse("7.8")},
					},
				},
			},
			CreatedBy: builder.CreatorMetadata{
				Name:    "Pack CLI",
				Version: "4.5.6",
			},
		}
	)
	it.Before(func() {
		cfg = config.Config{
			DefaultBuilder: "default/builder",
			RunImages: []config.RunImage{
				{Image: "some/run-image", Mirrors: []string{"first/local", "second/local"}},
			},
		}
		mockController = gomock.NewController(t)
		mockClient = testmocks.NewMockPackClient(mockController)
		logger = ilogging.NewLogWithWriters(&outBuf, &outBuf)

		command = commands.InspectBuilder(logger, cfg, mockClient)
	})

	it.After(func() {
		mockController.Finish()
	})

	when("#Get", func() {
		when("remote builder image cannot be found", func() {
			it("warns 'remote image not present'", func() {
				mockClient.EXPECT().InspectBuilder("some/image", false).Return(nil, nil)
				mockClient.EXPECT().InspectBuilder("some/image", true).Return(localInfo, nil)
				command.SetArgs([]string{"some/image"})
				assert.Succeeds(command.Execute())
				assert.Contains(outBuf.String(), `Inspecting builder: 'some/image'`)
				assert.Contains(outBuf.String(), "REMOTE:\n(not present)\n\n")
				assert.Contains(outBuf.String(), inspectBuilderLocalOutputSection)
			})
		})

		when("local builder image cannot be found", func() {
			it("warns 'local image not present'", func() {
				mockClient.EXPECT().InspectBuilder("some/image", false).Return(remoteInfo, nil)
				mockClient.EXPECT().InspectBuilder("some/image", true).Return(nil, nil)

				command.SetArgs([]string{"some/image"})
				assert.Succeeds(command.Execute())
				assert.Contains(outBuf.String(), `Inspecting builder: 'some/image'`)
				assert.Contains(outBuf.String(), "LOCAL:\n(not present)\n")

				assert.Contains(outBuf.String(), inspectBuilderRemoteOutputSection)
			})
		})

		when("image cannot be found", func() {
			it("logs 'errors when no image is found'", func() {
				mockClient.EXPECT().InspectBuilder("some/image", false).Return(nil, nil)
				mockClient.EXPECT().InspectBuilder("some/image", true).Return(nil, nil)

				command.SetArgs([]string{"some/image"})
				assert.ErrorContains(command.Execute(), "Unable to find builder 'some/image' locally or remotely.\n")
			})
		})

		when("inspector returns an error", func() {
			it("logs the error message", func() {
				mockClient.EXPECT().InspectBuilder("some/image", false).Return(nil, errors.New("some remote error"))
				mockClient.EXPECT().InspectBuilder("some/image", true).Return(nil, errors.New("some local error"))

				command.SetArgs([]string{"some/image"})
				assert.Succeeds(command.Execute())

				assert.Contains(outBuf.String(), `ERROR: inspecting remote image 'some/image': some remote error`)
				assert.Contains(outBuf.String(), `ERROR: inspecting local image 'some/image': some local error`)
			})
		})

		when("the image has empty fields in info", func() {
			it.Before(func() {
				mockClient.EXPECT().InspectBuilder("some/image", false).Return(&pack.BuilderInfo{
					Stack: "test.stack.id",
				}, nil)

				mockClient.EXPECT().InspectBuilder("some/image", true).Return(&pack.BuilderInfo{
					Stack: "test.stack.id",
				}, nil)

				command.SetArgs([]string{"some/image"})
			})

			it("missing creator info is skipped", func() {
				assert.Succeeds(command.Execute())
				assert.NotContains(outBuf.String(), "Created By:")
			})

			it("missing description is skipped", func() {
				assert.Succeeds(command.Execute())
				assert.NotContains(outBuf.String(), "Description:")
			})

			it("missing stack mixins are skipped", func() {
				assert.Succeeds(command.Execute())
				assert.NotContains(outBuf.String(), "Mixins")
			})

			it("missing buildpacks logs a warning", func() {
				assert.Succeeds(command.Execute())
				assert.Contains(outBuf.String(), "Buildpacks:\n  (none)")
				assert.Contains(outBuf.String(), "Warning: 'some/image' has no buildpacks")
				assert.Contains(outBuf.String(), "Users must supply buildpacks from the host machine")
			})

			it("missing groups logs a warning", func() {
				assert.Succeeds(command.Execute())
				assert.Contains(outBuf.String(), "Detection Order:\n  (none)")
				assert.Contains(outBuf.String(), "Warning: 'some/image' does not specify detection order")
				assert.Contains(outBuf.String(), "Users must build with explicitly specified buildpacks")
			})

			it("missing run image logs a warning", func() {
				assert.Succeeds(command.Execute())
				assert.Contains(outBuf.String(), "Run Images:\n  (none)")
				assert.Contains(outBuf.String(), "Warning: 'some/image' does not specify a run image")
				assert.Contains(outBuf.String(), "Users must build with an explicitly specified run image")
			})

			it("missing lifecycle version logs a warning", func() {
				assert.Succeeds(command.Execute())
				assert.Contains(outBuf.String(), "Warning: 'some/image' does not specify a Lifecycle version")
				assert.Contains(outBuf.String(), "Warning: 'some/image' does not specify supported Lifecycle Buildpack APIs")
				assert.Contains(outBuf.String(), "Warning: 'some/image' does not specify supported Lifecycle Platform APIs")
			})
		})

		when("is successful", func() {
			when("using the default builder", func() {
				it.Before(func() {
					cfg.DefaultBuilder = "some/image"
					mockClient.EXPECT().InspectBuilder("default/builder", false).Return(remoteInfo, nil)
					mockClient.EXPECT().InspectBuilder("default/builder", true).Return(localInfo, nil)
					command.SetArgs([]string{})
				})

				it("inspects the default builder", func() {
					assert.Succeeds(command.Execute())
					assert.Contains(outBuf.String(), "Inspecting default builder: 'default/builder'")
					assert.Contains(outBuf.String(), inspectBuilderRemoteOutputSection)
					assert.Contains(outBuf.String(), inspectBuilderLocalOutputSection)
				})
			})

			when("a builder arg is passed", func() {
				it.Before(func() {
					command.SetArgs([]string{"some/image"})
					mockClient.EXPECT().InspectBuilder("some/image", false).Return(remoteInfo, nil)
					mockClient.EXPECT().InspectBuilder("some/image", true).Return(localInfo, nil)
				})

				it("displays builder information for local and remote", func() {
					assert.Succeeds(command.Execute())
					assert.Contains(outBuf.String(), "Inspecting builder: 'some/image'")
					assert.Contains(outBuf.String(), inspectBuilderRemoteOutputSection)
					assert.Contains(outBuf.String(), inspectBuilderLocalOutputSection)
				})
			})

			when("the logger is verbose", func() {
				it.Before(func() {
					logger = ilogging.NewLogWithWriters(&outBuf, &outBuf, ilogging.WithVerbose())
					command = commands.InspectBuilder(logger, cfg, mockClient)

					cfg.DefaultBuilder = "some/image"
					mockClient.EXPECT().InspectBuilder("default/builder", false).Return(remoteInfo, nil)
					mockClient.EXPECT().InspectBuilder("default/builder", true).Return(localInfo, nil)
					command.SetArgs([]string{})
				})

				it("displays stack mixins", func() {
					assert.Succeeds(command.Execute())
					assert.Contains(outBuf.String(), stackLabelsSection)
				})
			})

			when("the builder is suggested", func() {
				it("indicates that it is trusted", func() {
					suggestedBuilder := "paketobuildpacks/builder:tiny"

					command.SetArgs([]string{suggestedBuilder})
					mockClient.EXPECT().InspectBuilder(suggestedBuilder, false).Return(remoteInfo, nil)
					mockClient.EXPECT().InspectBuilder(suggestedBuilder, true).Return(nil, nil)

					assert.Succeeds(command.Execute())
					assert.Contains(outBuf.String(), "Trusted: Yes")
				})
			})

			when("the builder has been trusted by the user", func() {
				it("indicated that it is trusted", func() {
					builderName := "trusted/builder"
					cfg.TrustedBuilders = []config.TrustedBuilder{{Name: builderName}}
					command = commands.InspectBuilder(logger, cfg, mockClient)

					command.SetArgs([]string{builderName})
					mockClient.EXPECT().InspectBuilder(builderName, false).Return(remoteInfo, nil)
					mockClient.EXPECT().InspectBuilder(builderName, true).Return(localInfo, nil)

					assert.Succeeds(command.Execute())
					assert.Contains(outBuf.String(), "Trusted: Yes")
				})
			})
		})

		when("default builder is not set", func() {
			when("no builder arg is passed", func() {
				it.Before(func() {
					command = commands.InspectBuilder(logger, config.Config{}, mockClient)
					command.SetArgs([]string{})

					// expect client to fetch suggested builder descriptions
					mockClient.EXPECT().InspectBuilder(gomock.Any(), false).Return(&pack.BuilderInfo{}, nil).AnyTimes()
				})

				it("informs the user", func() {
					assert.Fails(command.Execute())
					assert.Contains(outBuf.String(), selectDefaultBuilderOutput)
					assert.Matches(outBuf.String(), regexp.MustCompile(`Paketo Buildpacks:\s+'paketobuildpacks/builder:base'`))
					assert.Matches(outBuf.String(), regexp.MustCompile(`Paketo Buildpacks:\s+'paketobuildpacks/builder:full'`))
					assert.Matches(outBuf.String(), regexp.MustCompile(`Heroku:\s+'heroku/buildpacks:18'`))
				})
			})
		})

		when("a depth is specified", func() {
			it.Before(func() {
				command = commands.InspectBuilder(logger, config.Config{}, mockClient)
				command.SetArgs([]string{"some/image", "--depth", "2"})

				// expect client to fetch suggested builder descriptions
				mockClient.EXPECT().InspectBuilder("some/image", false).Return(remoteInfo, nil)
				mockClient.EXPECT().InspectBuilder("some/image", true).Return(localInfo, nil)
			})

			it("displays detection order up to the specified depth", func() {
				assert.Succeeds(command.Execute())

				assert.Contains(outBuf.String(), detectionOrderWithDepth)
			})
		})

		when("there is a cyclic buildpack dependency in the builder", func() {
			it.Before(func() {
				localInfo.BuildpackLayers = map[string]map[string]dist.BuildpackLayerInfo{
					"test.top.nested": {
						"test.top.nested.version": {
							API: apiVersion,
							Order: dist.Order{
								{
									Group: []dist.BuildpackRef{
										{
											BuildpackInfo: dist.BuildpackInfo{
												ID:      "test.nested",
												Version: "test.nested.version",
											},
											Optional: false,
										},
									},
								},
							},
							LayerDiffID: "sha256:test.top.nested.sha256",
						},
					},
					"test.nested": {
						"test.nested.version": {
							API: apiVersion,
							Order: dist.Order{
								{
									Group: []dist.BuildpackRef{
										{
											BuildpackInfo: dist.BuildpackInfo{
												// cyclic dependency here
												ID:      "test.top.nested",
												Version: "test.top.nested.version",
											},
											Optional: false,
										},
									},
								},
							},
							LayerDiffID: "sha256:test.nested.sha256",
							Homepage:    "http://geocities.com/top-bp",
						},
					},
					"test.bp.two": {
						"test.bp.two.version": {
							API: apiVersion,
							Stacks: []dist.Stack{
								{
									ID: "test.stack.id",
								},
							},
							LayerDiffID: "sha256:test.bp.two.sha256",
						},
					},
				}
				localInfo.Buildpacks = []dist.BuildpackInfo{
					{
						ID:      "test.top.nested",
						Version: "test.top.nested.version",
					},
					{
						ID:       "test.nested",
						Version:  "test.nested.version",
						Homepage: "http://geocities.com/top-bp",
					},
					{
						ID:      "test.bp.two",
						Version: "test.bp.two.version",
					},
				}
				localInfo.Order = dist.Order{
					{
						Group: []dist.BuildpackRef{
							{
								BuildpackInfo: dist.BuildpackInfo{ID: "test.top.nested", Version: "test.top.nested.version"},
								Optional:      false,
							},
							{
								BuildpackInfo: dist.BuildpackInfo{ID: "test.bp.two"},
								Optional:      true,
							},
						},
					},
					{
						Group: []dist.BuildpackRef{
							{
								BuildpackInfo: dist.BuildpackInfo{ID: "test.nested", Version: "test.nested.version"},
								Optional:      false,
							},
						},
					},
				}
			})

			it("indicates cycle and succeeds", func() {
				mockClient.EXPECT().InspectBuilder("some/image", false).Return(nil, nil)
				mockClient.EXPECT().InspectBuilder("some/image", true).Return(localInfo, nil)
				command.SetArgs([]string{"some/image"})

				assert.Succeeds(command.Execute())
				assert.AssertTrimmedContains(outBuf.String(), detectionOrderWithCycle)
			})
		})
	})
}
