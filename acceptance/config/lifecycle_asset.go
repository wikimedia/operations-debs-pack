// +build acceptance

package config

import (
	"strings"

	"github.com/Masterminds/semver"
	"github.com/buildpacks/lifecycle/api"

	"github.com/buildpacks/pack/internal/builder"
)

type LifecycleAsset struct {
	path       string
	descriptor builder.LifecycleDescriptor
}

func (a AssetManager) NewLifecycleAsset(kind ComboValue) LifecycleAsset {
	return LifecycleAsset{
		path:       a.LifecyclePath(kind),
		descriptor: a.LifecycleDescriptor(kind),
	}
}

func (l *LifecycleAsset) Version() string {
	return l.SemVer().String()
}

func (l *LifecycleAsset) SemVer() *builder.Version {
	return l.descriptor.Info.Version
}

func (l *LifecycleAsset) Identifier() string {
	if l.HasLocation() {
		return l.path
	} else {
		return l.Version()
	}
}

func (l *LifecycleAsset) HasLocation() bool {
	return l.path != ""
}

func (l *LifecycleAsset) EscapedPath() string {
	return strings.ReplaceAll(l.path, `\`, `\\`)
}

func earliestVersion(versions []*api.Version) *api.Version {
	var earliest *api.Version
	for _, version := range versions {
		switch {
		case version == nil:
			continue
		case earliest == nil:
			earliest = version
		case earliest.Compare(version) > 0:
			earliest = version
		}
	}
	return earliest
}

func (l *LifecycleAsset) EarliestBuildpackAPIVersion() string {
	return earliestVersion(l.descriptor.APIs.Buildpack.Supported).String()
}

func (l *LifecycleAsset) EarliestPlatformAPIVersion() string {
	return earliestVersion(l.descriptor.APIs.Platform.Supported).String()
}

func (l *LifecycleAsset) OutputForAPIs() (deprecatedBuildpackAPIs, supportedBuildpackAPIs, deprecatedPlatformAPIs, supportedPlatformAPIs string) {
	stringify := func(apiSet builder.APISet) string {
		versions := apiSet.AsStrings()
		if len(versions) == 0 {
			return "(none)"
		}
		return strings.Join(versions, ", ")
	}

	return stringify(l.descriptor.APIs.Buildpack.Deprecated),
		stringify(l.descriptor.APIs.Buildpack.Supported),
		stringify(l.descriptor.APIs.Platform.Deprecated),
		stringify(l.descriptor.APIs.Platform.Supported)
}

type LifecycleFeature int

const (
	CreatorInLifecycle LifecycleFeature = iota
)

var lifecycleFeatureTests = map[LifecycleFeature]func(l *LifecycleAsset) bool{
	CreatorInLifecycle: func(l *LifecycleAsset) bool {
		return l.atLeast074()
	},
}

func (l *LifecycleAsset) SupportsFeature(f LifecycleFeature) bool {
	return lifecycleFeatureTests[f](l)
}

func (l *LifecycleAsset) atLeast074() bool {
	return !l.SemVer().LessThan(semver.MustParse("0.7.4"))
}
