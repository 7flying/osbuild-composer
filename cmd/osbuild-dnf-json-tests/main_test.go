// This package contains tests related to dnf-json and rpmmd package.

//go:build integration

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/osbuild/osbuild-composer/internal/blueprint"
	"github.com/osbuild/osbuild-composer/internal/distro"
	rhel "github.com/osbuild/osbuild-composer/internal/distro/rhel8"
	"github.com/osbuild/osbuild-composer/internal/dnfjson"
	"github.com/osbuild/osbuild-composer/internal/rpmmd"
)

// This test loads all the repositories available in /repositories directory
// and tries to run depsolve for each architecture. With N architectures available
// this should run cross-arch dependency solving N-1 times.
func TestCrossArchDepsolve(t *testing.T) {
	// Load repositories from the definition we provide in the RPM package
	repoDir := "/usr/share/tests/osbuild-composer"

	// NOTE: we can add RHEL, but don't make it hard requirement because it will fail outside of VPN
	cs9 := rhel.NewCentos()

	// Set up temporary directory for rpm/dnf cache
	dir := t.TempDir()
	baseSolver := dnfjson.NewBaseSolver(dir)

	repos, err := rpmmd.LoadRepositories([]string{repoDir}, cs9.Name())
	require.NoErrorf(t, err, "Failed to LoadRepositories %v", cs9.Name())

	for _, archStr := range cs9.ListArches() {
		t.Run(archStr, func(t *testing.T) {
			arch, err := cs9.GetArch(archStr)
			require.NoError(t, err)
			solver := baseSolver.NewWithConfig(cs9.ModulePlatformID(), cs9.Releasever(), archStr, cs9.Name())
			for _, imgTypeStr := range arch.ListImageTypes() {
				t.Run(imgTypeStr, func(t *testing.T) {
					imgType, err := arch.GetImageType(imgTypeStr)
					require.NoError(t, err)

					packages := imgType.PackageSets(blueprint.Blueprint{},
						distro.ImageOptions{
							OSTree: distro.OSTreeImageOptions{
								URL:           "foo",
								ImageRef:      "bar",
								FetchChecksum: "baz",
							},
						},
						repos[archStr])

					for _, set := range packages {
						_, err = solver.Depsolve(set)
						assert.NoError(t, err)
					}
				})
			}
		})
	}
}

// This test loads all the repositories available in /repositories directory
// and tries to depsolve all package sets of one image type for one architecture.
func TestDepsolvePackageSets(t *testing.T) {
	// Load repositories from the definition we provide in the RPM package
	repoDir := "/usr/share/tests/osbuild-composer"

	// NOTE: we can add RHEL, but don't make it hard requirement because it will fail outside of VPN
	cs9 := rhel.NewCentos()

	// Set up temporary directory for rpm/dnf cache
	dir := t.TempDir()
	solver := dnfjson.NewSolver(cs9.ModulePlatformID(), cs9.Releasever(), distro.X86_64ArchName, cs9.Name(), dir)

	repos, err := rpmmd.LoadRepositories([]string{repoDir}, cs9.Name())
	require.NoErrorf(t, err, "Failed to LoadRepositories %v", cs9.Name())
	x86Repos, ok := repos[distro.X86_64ArchName]
	require.Truef(t, ok, "failed to get %q repos for %q", distro.X86_64ArchName, cs9.Name())

	x86Arch, err := cs9.GetArch(distro.X86_64ArchName)
	require.Nilf(t, err, "failed to get %q arch of %q distro", distro.X86_64ArchName, cs9.Name())

	qcow2ImageTypeName := "qcow2"
	qcow2Image, err := x86Arch.GetImageType(qcow2ImageTypeName)
	require.Nilf(t, err, "failed to get %q image type of %q/%q distro/arch", qcow2ImageTypeName, cs9.Name(), distro.X86_64ArchName)

	imagePkgSets := qcow2Image.PackageSets(blueprint.Blueprint{Packages: []blueprint.Package{{Name: "bind"}}}, distro.ImageOptions{}, x86Repos)

	gotPackageSpecsSets := make(map[string][]rpmmd.PackageSpec, len(imagePkgSets))
	for name, pkgSet := range imagePkgSets {
		res, err := solver.Depsolve(pkgSet)
		if err != nil {
			require.Nil(t, err)
		}
		gotPackageSpecsSets[name] = res
	}
	expectedPackageSpecsSetNames := []string{"build", "os"}
	require.EqualValues(t, len(expectedPackageSpecsSetNames), len(gotPackageSpecsSets))
	for _, name := range expectedPackageSpecsSetNames {
		_, ok := gotPackageSpecsSets[name]
		assert.True(t, ok)
	}
}
