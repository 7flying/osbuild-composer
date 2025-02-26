package fedora

import (
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"
	"golang.org/x/exp/slices"

	"github.com/osbuild/osbuild-composer/internal/blueprint"
	"github.com/osbuild/osbuild-composer/internal/common"
	"github.com/osbuild/osbuild-composer/internal/container"
	"github.com/osbuild/osbuild-composer/internal/disk"
	"github.com/osbuild/osbuild-composer/internal/distro"
	"github.com/osbuild/osbuild-composer/internal/environment"
	"github.com/osbuild/osbuild-composer/internal/image"
	"github.com/osbuild/osbuild-composer/internal/manifest"
	"github.com/osbuild/osbuild-composer/internal/oscap"
	"github.com/osbuild/osbuild-composer/internal/pathpolicy"
	"github.com/osbuild/osbuild-composer/internal/platform"
	"github.com/osbuild/osbuild-composer/internal/rpmmd"
	"github.com/osbuild/osbuild-composer/internal/runner"
	"github.com/osbuild/osbuild-composer/internal/workload"
)

const (
	// package set names

	// main/common os image package set name
	osPkgsKey = "os"

	// container package set name
	containerPkgsKey = "container"

	// installer package set name
	installerPkgsKey = "installer"

	// blueprint package set name
	blueprintPkgsKey = "blueprint"

	//Kernel options for ami, qcow2, openstack, vhd and vmdk types
	defaultKernelOptions = "ro no_timer_check console=ttyS0,115200n8 biosdevname=0 net.ifnames=0"
)

var (
	oscapProfileAllowList = []oscap.Profile{
		oscap.Ospp,
		oscap.PciDss,
		oscap.Standard,
	}

	// Services
	iotServices = []string{
		"NetworkManager.service",
		"firewalld.service",
		"rngd.service",
		"sshd.service",
		"zezere_ignition.timer",
		"zezere_ignition_banner.service",
		"greenboot-grub2-set-counter",
		"greenboot-grub2-set-success",
		"greenboot-healthcheck",
		"greenboot-rpm-ostree-grub2-check-fallback",
		"greenboot-status",
		"greenboot-task-runner",
		"redboot-auto-reboot",
		"redboot-task-runner",
		"parsec",
		"dbus-parsec",
	}

	// Image Definitions
	imageInstallerImgType = imageType{
		name:        "image-installer",
		nameAliases: []string{"fedora-image-installer"},
		filename:    "installer.iso",
		mimeType:    "application/x-iso9660-image",
		packageSets: map[string]packageSetFunc{
			osPkgsKey:        minimalrpmPackageSet,
			installerPkgsKey: imageInstallerPackageSet,
		},
		bootable:         true,
		bootISO:          true,
		rpmOstree:        false,
		image:            imageInstallerImage,
		buildPipelines:   []string{"build"},
		payloadPipelines: []string{"anaconda-tree", "rootfs-image", "efiboot-tree", "os", "bootiso-tree", "bootiso"},
		exports:          []string{"bootiso"},
	}

	iotCommitImgType = imageType{
		name:        "iot-commit",
		nameAliases: []string{"fedora-iot-commit"},
		filename:    "commit.tar",
		mimeType:    "application/x-tar",
		packageSets: map[string]packageSetFunc{
			osPkgsKey: iotCommitPackageSet,
		},
		defaultImageConfig: &distro.ImageConfig{
			EnabledServices: iotServices,
		},
		rpmOstree:        true,
		image:            iotCommitImage,
		buildPipelines:   []string{"build"},
		payloadPipelines: []string{"os", "ostree-commit", "commit-archive"},
		exports:          []string{"commit-archive"},
	}

	iotOCIImgType = imageType{
		name:        "iot-container",
		nameAliases: []string{"fedora-iot-container"},
		filename:    "container.tar",
		mimeType:    "application/x-tar",
		packageSets: map[string]packageSetFunc{
			osPkgsKey: iotCommitPackageSet,
			containerPkgsKey: func(t *imageType) rpmmd.PackageSet {
				return rpmmd.PackageSet{}
			},
		},
		defaultImageConfig: &distro.ImageConfig{
			EnabledServices: iotServices,
		},
		rpmOstree:        true,
		bootISO:          false,
		image:            iotContainerImage,
		buildPipelines:   []string{"build"},
		payloadPipelines: []string{"os", "ostree-commit", "container-tree", "container"},
		exports:          []string{"container"},
	}

	iotInstallerImgType = imageType{
		name:        "iot-installer",
		nameAliases: []string{"fedora-iot-installer"},
		filename:    "installer.iso",
		mimeType:    "application/x-iso9660-image",
		packageSets: map[string]packageSetFunc{
			installerPkgsKey: iotInstallerPackageSet,
		},
		defaultImageConfig: &distro.ImageConfig{
			Locale:          common.ToPtr("en_US.UTF-8"),
			EnabledServices: iotServices,
		},
		rpmOstree:        true,
		bootISO:          true,
		image:            iotInstallerImage,
		buildPipelines:   []string{"build"},
		payloadPipelines: []string{"anaconda-tree", "rootfs-image", "efiboot-tree", "bootiso-tree", "bootiso"},
		exports:          []string{"bootiso"},
	}

	iotRawImgType = imageType{
		name:        "iot-raw-image",
		nameAliases: []string{"fedora-iot-raw-image"},
		filename:    "image.raw.xz",
		mimeType:    "application/xz",
		packageSets: map[string]packageSetFunc{},
		defaultImageConfig: &distro.ImageConfig{
			Locale: common.ToPtr("en_US.UTF-8"),
		},
		defaultSize:         4 * common.GibiByte,
		rpmOstree:           true,
		bootable:            true,
		image:               iotRawImage,
		buildPipelines:      []string{"build"},
		payloadPipelines:    []string{"ostree-deployment", "image", "xz"},
		exports:             []string{"xz"},
		basePartitionTables: iotBasePartitionTables,

		// Passing an empty map into the required partition sizes disables the
		// default partition sizes normally set so our `basePartitionTables` can
		// override them (and make them smaller, in this case).
		requiredPartitionSizes: map[string]uint64{},
	}

	qcow2ImgType = imageType{
		name:     "qcow2",
		filename: "disk.qcow2",
		mimeType: "application/x-qemu-disk",
		packageSets: map[string]packageSetFunc{
			osPkgsKey: qcow2CommonPackageSet,
		},
		defaultImageConfig: &distro.ImageConfig{
			DefaultTarget: common.ToPtr("multi-user.target"),
			EnabledServices: []string{
				"cloud-init.service",
				"cloud-config.service",
				"cloud-final.service",
				"cloud-init-local.service",
			},
		},
		kernelOptions:       defaultKernelOptions,
		bootable:            true,
		defaultSize:         2 * common.GibiByte,
		image:               liveImage,
		buildPipelines:      []string{"build"},
		payloadPipelines:    []string{"os", "image", "qcow2"},
		exports:             []string{"qcow2"},
		basePartitionTables: defaultBasePartitionTables,
	}

	vhdImgType = imageType{
		name:     "vhd",
		filename: "disk.vhd",
		mimeType: "application/x-vhd",
		packageSets: map[string]packageSetFunc{
			osPkgsKey: vhdCommonPackageSet,
		},
		defaultImageConfig: &distro.ImageConfig{
			Locale: common.ToPtr("en_US.UTF-8"),
			EnabledServices: []string{
				"sshd",
			},
			DefaultTarget: common.ToPtr("multi-user.target"),
			DisabledServices: []string{
				"proc-sys-fs-binfmt_misc.mount",
				"loadmodules.service",
			},
		},
		kernelOptions:       defaultKernelOptions,
		bootable:            true,
		defaultSize:         2 * common.GibiByte,
		image:               liveImage,
		buildPipelines:      []string{"build"},
		payloadPipelines:    []string{"os", "image", "vpc"},
		exports:             []string{"vpc"},
		basePartitionTables: defaultBasePartitionTables,
		environment:         &environment.Azure{},
	}

	vmdkDefaultImageConfig = &distro.ImageConfig{
		Locale: common.ToPtr("en_US.UTF-8"),
		EnabledServices: []string{
			"cloud-init.service",
			"cloud-config.service",
			"cloud-final.service",
			"cloud-init-local.service",
		},
	}

	vmdkImgType = imageType{
		name:     "vmdk",
		filename: "disk.vmdk",
		mimeType: "application/x-vmdk",
		packageSets: map[string]packageSetFunc{
			osPkgsKey: vmdkCommonPackageSet,
		},
		defaultImageConfig:  vmdkDefaultImageConfig,
		kernelOptions:       defaultKernelOptions,
		bootable:            true,
		defaultSize:         2 * common.GibiByte,
		image:               liveImage,
		buildPipelines:      []string{"build"},
		payloadPipelines:    []string{"os", "image", "vmdk"},
		exports:             []string{"vmdk"},
		basePartitionTables: defaultBasePartitionTables,
	}

	ovaImgType = imageType{
		name:     "ova",
		filename: "image.ova",
		mimeType: "application/ovf",
		packageSets: map[string]packageSetFunc{
			osPkgsKey: vmdkCommonPackageSet,
		},
		defaultImageConfig:  vmdkDefaultImageConfig,
		kernelOptions:       defaultKernelOptions,
		bootable:            true,
		defaultSize:         2 * common.GibiByte,
		image:               liveImage,
		buildPipelines:      []string{"build"},
		payloadPipelines:    []string{"os", "image", "vmdk", "ovf", "archive"},
		exports:             []string{"archive"},
		basePartitionTables: defaultBasePartitionTables,
	}

	openstackImgType = imageType{
		name:     "openstack",
		filename: "disk.qcow2",
		mimeType: "application/x-qemu-disk",
		packageSets: map[string]packageSetFunc{
			osPkgsKey: openstackCommonPackageSet,
		},
		defaultImageConfig: &distro.ImageConfig{
			Locale: common.ToPtr("en_US.UTF-8"),
			EnabledServices: []string{
				"cloud-init.service",
				"cloud-config.service",
				"cloud-final.service",
				"cloud-init-local.service",
			},
		},
		kernelOptions:       defaultKernelOptions,
		bootable:            true,
		defaultSize:         2 * common.GibiByte,
		image:               liveImage,
		buildPipelines:      []string{"build"},
		payloadPipelines:    []string{"os", "image", "qcow2"},
		exports:             []string{"qcow2"},
		basePartitionTables: defaultBasePartitionTables,
	}

	// default EC2 images config (common for all architectures)
	defaultEc2ImageConfig = &distro.ImageConfig{
		DefaultTarget: common.ToPtr("multi-user.target"),
	}

	amiImgType = imageType{
		name:     "ami",
		filename: "image.raw",
		mimeType: "application/octet-stream",
		packageSets: map[string]packageSetFunc{
			osPkgsKey: ec2CommonPackageSet,
		},
		defaultImageConfig:  defaultEc2ImageConfig,
		kernelOptions:       defaultKernelOptions,
		bootable:            true,
		defaultSize:         6 * common.GibiByte,
		image:               liveImage,
		buildPipelines:      []string{"build"},
		payloadPipelines:    []string{"os", "image"},
		exports:             []string{"image"},
		basePartitionTables: defaultBasePartitionTables,
		environment:         &environment.EC2{},
	}

	containerImgType = imageType{
		name:     "container",
		filename: "container.tar",
		mimeType: "application/x-tar",
		packageSets: map[string]packageSetFunc{
			osPkgsKey: containerPackageSet,
		},
		defaultImageConfig: &distro.ImageConfig{
			NoSElinux:   common.ToPtr(true),
			ExcludeDocs: common.ToPtr(true),
			Locale:      common.ToPtr("C.UTF-8"),
			Timezone:    common.ToPtr("Etc/UTC"),
		},
		image:            containerImage,
		bootable:         false,
		buildPipelines:   []string{"build"},
		payloadPipelines: []string{"os", "container"},
		exports:          []string{"container"},
	}

	minimalrawImgType = imageType{
		name:     "minimal-raw",
		filename: "raw.img",
		mimeType: "application/disk",
		packageSets: map[string]packageSetFunc{
			osPkgsKey: minimalrpmPackageSet,
		},
		rpmOstree:           false,
		kernelOptions:       defaultKernelOptions,
		bootable:            true,
		defaultSize:         2 * common.GibiByte,
		image:               liveImage,
		buildPipelines:      []string{"build"},
		payloadPipelines:    []string{"os", "image"},
		exports:             []string{"image"},
		basePartitionTables: defaultBasePartitionTables,
	}
)

type distribution struct {
	name               string
	product            string
	osVersion          string
	releaseVersion     string
	modulePlatformID   string
	ostreeRefTmpl      string
	isolabelTmpl       string
	runner             runner.Runner
	arches             map[string]distro.Arch
	defaultImageConfig *distro.ImageConfig
}

// Fedora based OS image configuration defaults
var defaultDistroImageConfig = &distro.ImageConfig{
	Timezone: common.ToPtr("UTC"),
	Locale:   common.ToPtr("en_US"),
}

func getDistro(version int) distribution {
	return distribution{
		name:               fmt.Sprintf("fedora-%d", version),
		product:            "Fedora",
		osVersion:          strconv.Itoa(version),
		releaseVersion:     strconv.Itoa(version),
		modulePlatformID:   fmt.Sprintf("platform:f%d", version),
		ostreeRefTmpl:      fmt.Sprintf("fedora/%d/%%s/iot", version),
		isolabelTmpl:       fmt.Sprintf("Fedora-%d-BaseOS-%%s", version),
		runner:             &runner.Fedora{Version: uint64(version)},
		defaultImageConfig: defaultDistroImageConfig,
	}
}

func (d *distribution) Name() string {
	return d.name
}

func (d *distribution) Releasever() string {
	return d.releaseVersion
}

func (d *distribution) ModulePlatformID() string {
	return d.modulePlatformID
}

func (d *distribution) OSTreeRef() string {
	return d.ostreeRefTmpl
}

func (d *distribution) ListArches() []string {
	archNames := make([]string, 0, len(d.arches))
	for name := range d.arches {
		archNames = append(archNames, name)
	}
	sort.Strings(archNames)
	return archNames
}

func (d *distribution) GetArch(name string) (distro.Arch, error) {
	arch, exists := d.arches[name]
	if !exists {
		return nil, errors.New("invalid architecture: " + name)
	}
	return arch, nil
}

func (d *distribution) addArches(arches ...architecture) {
	if d.arches == nil {
		d.arches = map[string]distro.Arch{}
	}

	// Do not make copies of architectures, as opposed to image types,
	// because architecture definitions are not used by more than a single
	// distro definition.
	for idx := range arches {
		d.arches[arches[idx].name] = &arches[idx]
	}
}

func (d *distribution) getDefaultImageConfig() *distro.ImageConfig {
	return d.defaultImageConfig
}

type architecture struct {
	distro           *distribution
	name             string
	imageTypes       map[string]distro.ImageType
	imageTypeAliases map[string]string
}

func (a *architecture) Name() string {
	return a.name
}

func (a *architecture) ListImageTypes() []string {
	itNames := make([]string, 0, len(a.imageTypes))
	for name := range a.imageTypes {
		itNames = append(itNames, name)
	}
	sort.Strings(itNames)
	return itNames
}

func (a *architecture) GetImageType(name string) (distro.ImageType, error) {
	t, exists := a.imageTypes[name]
	if !exists {
		aliasForName, exists := a.imageTypeAliases[name]
		if !exists {
			return nil, errors.New("invalid image type: " + name)
		}
		t, exists = a.imageTypes[aliasForName]
		if !exists {
			panic(fmt.Sprintf("image type '%s' is an alias to a non-existing image type '%s'", name, aliasForName))
		}
	}
	return t, nil
}

func (a *architecture) addImageTypes(platform platform.Platform, imageTypes ...imageType) {
	if a.imageTypes == nil {
		a.imageTypes = map[string]distro.ImageType{}
	}
	for idx := range imageTypes {
		it := imageTypes[idx]
		it.arch = a
		it.platform = platform
		a.imageTypes[it.name] = &it
		for _, alias := range it.nameAliases {
			if a.imageTypeAliases == nil {
				a.imageTypeAliases = map[string]string{}
			}
			if existingAliasFor, exists := a.imageTypeAliases[alias]; exists {
				panic(fmt.Sprintf("image type alias '%s' for '%s' is already defined for another image type '%s'", alias, it.name, existingAliasFor))
			}
			a.imageTypeAliases[alias] = it.name
		}
	}
}

func (a *architecture) Distro() distro.Distro {
	return a.distro
}

type imageFunc func(workload workload.Workload, t *imageType, customizations *blueprint.Customizations, options distro.ImageOptions, packageSets map[string]rpmmd.PackageSet, containers []container.Spec, rng *rand.Rand) (image.ImageKind, error)

type packageSetFunc func(t *imageType) rpmmd.PackageSet

type imageType struct {
	arch               *architecture
	platform           platform.Platform
	environment        environment.Environment
	name               string
	nameAliases        []string
	filename           string
	mimeType           string
	packageSets        map[string]packageSetFunc
	defaultImageConfig *distro.ImageConfig
	kernelOptions      string
	defaultSize        uint64
	buildPipelines     []string
	payloadPipelines   []string
	exports            []string
	image              imageFunc

	// bootISO: installable ISO
	bootISO bool
	// rpmOstree: iot/ostree
	rpmOstree bool
	// bootable image
	bootable bool
	// List of valid arches for the image type
	basePartitionTables    distro.BasePartitionTableMap
	requiredPartitionSizes map[string]uint64
}

func (t *imageType) Name() string {
	return t.name
}

func (t *imageType) Arch() distro.Arch {
	return t.arch
}

func (t *imageType) Filename() string {
	return t.filename
}

func (t *imageType) MIMEType() string {
	return t.mimeType
}

func (t *imageType) OSTreeRef() string {
	d := t.arch.distro
	if t.rpmOstree {
		return fmt.Sprintf(d.ostreeRefTmpl, t.arch.Name())
	}
	return ""
}

func (t *imageType) Size(size uint64) uint64 {
	// Microsoft Azure requires vhd images to be rounded up to the nearest MB
	if t.name == "vhd" && size%common.MebiByte != 0 {
		size = (size/common.MebiByte + 1) * common.MebiByte
	}
	if size == 0 {
		size = t.defaultSize
	}
	return size
}

func (t *imageType) PackageSets(bp blueprint.Blueprint, options distro.ImageOptions, repos []rpmmd.RepoConfig) map[string][]rpmmd.PackageSet {
	// merge package sets that appear in the image type with the package sets
	// of the same name from the distro and arch
	packageSets := make(map[string]rpmmd.PackageSet)

	for name, getter := range t.packageSets {
		packageSets[name] = getter(t)
	}

	// amend with repository information
	for _, repo := range repos {
		if len(repo.PackageSets) > 0 {
			// only apply the repo to the listed package sets
			for _, psName := range repo.PackageSets {
				ps := packageSets[psName]
				ps.Repositories = append(ps.Repositories, repo)
				packageSets[psName] = ps
			}
		}
	}

	// In case of Cloud API, this method is called before the ostree commit
	// is resolved. Unfortunately, initializeManifest when called for
	// an ostree installer returns an error.
	//
	// Work around this by providing a dummy FetchChecksum to convince the
	// method that it's fine to initialize the manifest. Note that the ostree
	// content has no effect on the package sets, so this is fine.
	//
	// See: https://github.com/osbuild/osbuild-composer/issues/3125
	//
	// TODO: Remove me when it's possible the get the package set chain without
	//       resolving the ostree reference before. Also remove the test for
	//       this workaround
	if t.rpmOstree && t.bootISO && options.OSTree.FetchChecksum == "" {
		options.OSTree.FetchChecksum = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
		logrus.Warn("FIXME: Requesting package sets for iot-installer without a resolved ostree ref. Faking one.")
	}

	// Similar to above, for edge-commit and edge-container, we need to set an
	// ImageRef in order to properly initialize the manifest and package
	// selection.
	options.OSTree.ImageRef = t.OSTreeRef()

	// create a temporary container spec array with the info from the blueprint
	// to initialize the manifest
	containers := make([]container.Spec, len(bp.Containers))
	for idx := range bp.Containers {
		containers[idx] = container.Spec{
			Source:    bp.Containers[idx].Source,
			TLSVerify: bp.Containers[idx].TLSVerify,
			LocalName: bp.Containers[idx].Name,
		}
	}

	// create a manifest object and instantiate it with the computed packageSetChains
	manifest, _, err := t.initializeManifest(&bp, options, repos, packageSets, containers, 0)
	if err != nil {
		// TODO: handle manifest initialization errors more gracefully, we
		// refuse to initialize manifests with invalid config.
		logrus.Errorf("Initializing the manifest failed for %s (%s/%s): %v", t.Name(), t.arch.distro.Name(), t.arch.Name(), err)
		return nil
	}

	return manifest.GetPackageSetChains()
}

func (t *imageType) BuildPipelines() []string {
	return t.buildPipelines
}

func (t *imageType) PayloadPipelines() []string {
	return t.payloadPipelines
}

func (t *imageType) PayloadPackageSets() []string {
	return []string{blueprintPkgsKey}
}

func (t *imageType) PackageSetsChains() map[string][]string {
	return make(map[string][]string)
}

func (t *imageType) Exports() []string {
	if len(t.exports) > 0 {
		return t.exports
	}
	return []string{"assembler"}
}

func (t *imageType) BootMode() distro.BootMode {
	if t.platform.GetUEFIVendor() != "" && t.platform.GetBIOSPlatform() != "" {
		return distro.BOOT_HYBRID
	} else if t.platform.GetUEFIVendor() != "" {
		return distro.BOOT_UEFI
	} else if t.platform.GetBIOSPlatform() != "" || t.platform.GetZiplSupport() {
		return distro.BOOT_LEGACY
	}
	return distro.BOOT_NONE
}

func (t *imageType) getPartitionTable(
	mountpoints []blueprint.FilesystemCustomization,
	options distro.ImageOptions,
	rng *rand.Rand,
) (*disk.PartitionTable, error) {
	basePartitionTable, exists := t.basePartitionTables[t.arch.Name()]
	if !exists {
		return nil, fmt.Errorf("unknown arch: " + t.arch.Name())
	}

	imageSize := t.Size(options.Size)

	lvmify := !t.rpmOstree

	return disk.NewPartitionTable(&basePartitionTable, mountpoints, imageSize, lvmify, t.requiredPartitionSizes, rng)
}

func (t *imageType) getDefaultImageConfig() *distro.ImageConfig {
	// ensure that image always returns non-nil default config
	imageConfig := t.defaultImageConfig
	if imageConfig == nil {
		imageConfig = &distro.ImageConfig{}
	}
	return imageConfig.InheritFrom(t.arch.distro.getDefaultImageConfig())

}

func (t *imageType) PartitionType() string {
	basePartitionTable, exists := t.basePartitionTables[t.arch.Name()]
	if !exists {
		return ""
	}

	return basePartitionTable.Type
}

func (t *imageType) initializeManifest(bp *blueprint.Blueprint,
	options distro.ImageOptions,
	repos []rpmmd.RepoConfig,
	packageSets map[string]rpmmd.PackageSet,
	containers []container.Spec,
	seed int64) (*manifest.Manifest, []string, error) {

	warnings, err := t.checkOptions(bp.Customizations, options, containers)
	if err != nil {
		return nil, nil, err
	}

	// TODO: let image types specify valid workloads, rather than
	// always assume Custom.
	w := &workload.Custom{
		BaseWorkload: workload.BaseWorkload{
			Repos: packageSets[blueprintPkgsKey].Repositories,
		},
		Packages: bp.GetPackagesEx(false),
	}
	if services := bp.Customizations.GetServices(); services != nil {
		w.Services = services.Enabled
		w.DisabledServices = services.Disabled
	}

	source := rand.NewSource(seed)
	// math/rand is good enough in this case
	/* #nosec G404 */
	rng := rand.New(source)

	img, err := t.image(w, t, bp.Customizations, options, packageSets, containers, rng)
	if err != nil {
		return nil, nil, err
	}
	manifest := manifest.New()
	_, err = img.InstantiateManifest(&manifest, repos, t.arch.distro.runner, rng)
	if err != nil {
		return nil, nil, err
	}
	return &manifest, warnings, err
}

func (t *imageType) Manifest(customizations *blueprint.Customizations,
	options distro.ImageOptions,
	repos []rpmmd.RepoConfig,
	packageSets map[string][]rpmmd.PackageSpec,
	containers []container.Spec,
	seed int64) (distro.Manifest, []string, error) {

	bp := &blueprint.Blueprint{Name: "empty blueprint"}
	err := bp.Initialize()
	if err != nil {
		panic("could not initialize empty blueprint: " + err.Error())
	}
	bp.Customizations = customizations

	// the os pipeline filters repos based on the `osPkgsKey` package set, merge the repos which
	// contain a payload package set into the `osPkgsKey`, so those repos are included when
	// building the rpm stage in the os pipeline
	// TODO: roll this into workloads
	mergedRepos := make([]rpmmd.RepoConfig, 0, len(repos))
	for _, repo := range repos {
		for _, pkgsKey := range t.PayloadPackageSets() {
			// If the repo already contains the osPkgsKey, skip
			if slices.Contains(repo.PackageSets, osPkgsKey) {
				break
			}
			if slices.Contains(repo.PackageSets, pkgsKey) {
				repo.PackageSets = append(repo.PackageSets, osPkgsKey)
			}
		}
		mergedRepos = append(mergedRepos, repo)
	}

	manifest, warnings, err := t.initializeManifest(bp, options, mergedRepos, nil, containers, seed)
	if err != nil {
		return distro.Manifest{}, nil, err
	}

	ret, err := manifest.Serialize(packageSets)
	if err != nil {
		return ret, nil, err
	}
	return ret, warnings, err
}

// checkOptions checks the validity and compatibility of options and customizations for the image type.
// Returns ([]string, error) where []string, if non-nil, will hold any generated warnings (e.g. deprecation notices).
func (t *imageType) checkOptions(customizations *blueprint.Customizations, options distro.ImageOptions, containers []container.Spec) ([]string, error) {

	// we do not support embedding containers on ostree-derived images, only on commits themselves
	if len(containers) > 0 && t.rpmOstree && (t.name != "iot-commit" && t.name != "iot-container") {
		return nil, fmt.Errorf("embedding containers is not supported for %s on %s", t.name, t.arch.distro.name)
	}

	if t.bootISO && t.rpmOstree {
		// check the checksum instead of the URL, because the URL should have been used to resolve the checksum and we need both
		if options.OSTree.FetchChecksum == "" {
			return nil, fmt.Errorf("boot ISO image type %q requires specifying a URL from which to retrieve the OSTree commit", t.name)
		}
	}

	if t.name == "iot-raw-image" {
		allowed := []string{"User", "Group", "Directories", "Files", "Services"}
		if err := customizations.CheckAllowed(allowed...); err != nil {
			return nil, fmt.Errorf("unsupported blueprint customizations found for image type %q: (allowed: %s)", t.name, strings.Join(allowed, ", "))
		}
		// TODO: consider additional checks, such as those in "edge-simplified-installer" in RHEL distros
	}

	// BootISO's have limited support for customizations.
	// TODO: Support kernel name selection for image-installer
	if t.bootISO {
		if t.name == "iot-installer" || t.name == "image-installer" {
			allowed := []string{"User", "Group"}
			if err := customizations.CheckAllowed(allowed...); err != nil {
				return nil, fmt.Errorf("unsupported blueprint customizations found for boot ISO image type %q: (allowed: %s)", t.name, strings.Join(allowed, ", "))
			}
		}
	}

	if kernelOpts := customizations.GetKernel(); kernelOpts.Append != "" && t.rpmOstree {
		return nil, fmt.Errorf("kernel boot parameter customizations are not supported for ostree types")
	}

	mountpoints := customizations.GetFilesystems()

	if mountpoints != nil && t.rpmOstree {
		return nil, fmt.Errorf("Custom mountpoints are not supported for ostree types")
	}

	err := blueprint.CheckMountpointsPolicy(mountpoints, pathpolicy.MountpointPolicies)
	if err != nil {
		return nil, err
	}

	if osc := customizations.GetOpenSCAP(); osc != nil {
		supported := oscap.IsProfileAllowed(osc.ProfileID, oscapProfileAllowList)
		if !supported {
			return nil, fmt.Errorf(fmt.Sprintf("OpenSCAP unsupported profile: %s", osc.ProfileID))
		}
		if t.rpmOstree {
			return nil, fmt.Errorf("OpenSCAP customizations are not supported for ostree types")
		}
		if osc.DataStream == "" {
			return nil, fmt.Errorf("OpenSCAP datastream cannot be empty")
		}
		if osc.ProfileID == "" {
			return nil, fmt.Errorf("OpenSCAP profile cannot be empty")
		}
	}

	// Check Directory/File Customizations are valid
	dc := customizations.GetDirectories()
	fc := customizations.GetFiles()

	err = blueprint.ValidateDirFileCustomizations(dc, fc)
	if err != nil {
		return nil, err
	}

	err = blueprint.CheckDirectoryCustomizationsPolicy(dc, pathpolicy.CustomDirectoriesPolicies)
	if err != nil {
		return nil, err
	}

	err = blueprint.CheckFileCustomizationsPolicy(fc, pathpolicy.CustomFilesPolicies)
	if err != nil {
		return nil, err
	}

	// check if repository customizations are valid
	_, err = customizations.GetRepositories()
	if err != nil {
		return nil, err
	}

	return nil, nil
}

// New creates a new distro object, defining the supported architectures and image types
func NewF36() distro.Distro {
	return newDistro(36)
}
func NewF37() distro.Distro {
	return newDistro(37)
}
func NewF38() distro.Distro {
	return newDistro(38)
}
func NewF39() distro.Distro {
	return newDistro(39)
}

func newDistro(version int) distro.Distro {
	rd := getDistro(version)

	// Architecture definitions
	x86_64 := architecture{
		name:   distro.X86_64ArchName,
		distro: &rd,
	}

	aarch64 := architecture{
		name:   distro.Aarch64ArchName,
		distro: &rd,
	}

	ociImgType := qcow2ImgType
	ociImgType.name = "oci"

	x86_64.addImageTypes(
		&platform.X86{
			BIOS:       true,
			UEFIVendor: "fedora",
			BasePlatform: platform.BasePlatform{
				ImageFormat: platform.FORMAT_QCOW2,
				QCOW2Compat: "1.1",
			},
		},
		qcow2ImgType,
		ociImgType,
	)
	x86_64.addImageTypes(
		&platform.X86{
			BIOS:       true,
			UEFIVendor: "fedora",
			BasePlatform: platform.BasePlatform{
				ImageFormat: platform.FORMAT_QCOW2,
			},
		},
		openstackImgType,
	)
	x86_64.addImageTypes(
		&platform.X86{
			BIOS:       true,
			UEFIVendor: "fedora",
			BasePlatform: platform.BasePlatform{
				ImageFormat: platform.FORMAT_VHD,
			},
		},
		vhdImgType,
	)
	x86_64.addImageTypes(
		&platform.X86{
			BIOS:       true,
			UEFIVendor: "fedora",
			BasePlatform: platform.BasePlatform{
				ImageFormat: platform.FORMAT_VMDK,
			},
		},
		vmdkImgType,
	)
	x86_64.addImageTypes(
		&platform.X86{
			BIOS:       true,
			UEFIVendor: "fedora",
			BasePlatform: platform.BasePlatform{
				ImageFormat: platform.FORMAT_OVA,
			},
		},
		ovaImgType,
	)
	x86_64.addImageTypes(
		&platform.X86{
			BIOS:       true,
			UEFIVendor: "fedora",
			BasePlatform: platform.BasePlatform{
				ImageFormat: platform.FORMAT_RAW,
			},
		},
		amiImgType,
	)
	x86_64.addImageTypes(
		&platform.X86{},
		containerImgType,
	)
	x86_64.addImageTypes(
		&platform.X86{
			BasePlatform: platform.BasePlatform{
				FirmwarePackages: []string{
					"microcode_ctl", // ??
					"iwl1000-firmware",
					"iwl100-firmware",
					"iwl105-firmware",
					"iwl135-firmware",
					"iwl2000-firmware",
					"iwl2030-firmware",
					"iwl3160-firmware",
					"iwl5000-firmware",
					"iwl5150-firmware",
					"iwl6000-firmware",
					"iwl6050-firmware",
				},
			},
			BIOS:       true,
			UEFIVendor: "fedora",
		},
		iotOCIImgType,
		iotCommitImgType,
		iotInstallerImgType,
		imageInstallerImgType,
	)
	x86_64.addImageTypes(
		&platform.X86{
			BasePlatform: platform.BasePlatform{
				ImageFormat: platform.FORMAT_RAW,
			},
			BIOS:       false,
			UEFIVendor: "fedora",
		},
		iotRawImgType,
	)
	aarch64.addImageTypes(
		&platform.Aarch64{
			UEFIVendor: "fedora",
			BasePlatform: platform.BasePlatform{
				ImageFormat: platform.FORMAT_RAW,
			},
		},
		amiImgType,
	)
	aarch64.addImageTypes(
		&platform.Aarch64{
			UEFIVendor: "fedora",
			BasePlatform: platform.BasePlatform{
				ImageFormat: platform.FORMAT_QCOW2,
				QCOW2Compat: "1.1",
			},
		},
		qcow2ImgType,
		ociImgType,
	)
	aarch64.addImageTypes(
		&platform.Aarch64{
			UEFIVendor: "fedora",
			BasePlatform: platform.BasePlatform{
				ImageFormat: platform.FORMAT_QCOW2,
			},
		},
		openstackImgType,
	)
	aarch64.addImageTypes(
		&platform.Aarch64{},
		containerImgType,
	)
	aarch64.addImageTypes(
		&platform.Aarch64{
			BasePlatform: platform.BasePlatform{
				FirmwarePackages: []string{
					"uboot-images-armv8", // ??
					"bcm283x-firmware",
					"arm-image-installer", // ??
				},
			},
			UEFIVendor: "fedora",
		},
		iotCommitImgType,
		iotOCIImgType,
		iotInstallerImgType,
		imageInstallerImgType,
	)
	aarch64.addImageTypes(
		&platform.Aarch64_IoT{
			BasePlatform: platform.BasePlatform{
				ImageFormat: platform.FORMAT_RAW,
			},
			UEFIVendor: "fedora",
			BootFiles: [][2]string{
				{"/usr/lib/ostree-boot/efi/bcm2710-rpi-2-b.dtb", "/boot/efi/"},
				{"/usr/lib/ostree-boot/efi/bcm2710-rpi-3-b-plus.dtb", "/boot/efi/"},
				{"/usr/lib/ostree-boot/efi/bcm2710-rpi-3-b.dtb", "/boot/efi/"},
				{"/usr/lib/ostree-boot/efi/bcm2710-rpi-cm3.dtb", "/boot/efi/"},
				{"/usr/lib/ostree-boot/efi/bcm2710-rpi-zero-2-w.dtb", "/boot/efi/"},
				{"/usr/lib/ostree-boot/efi/bcm2710-rpi-zero-2.dtb", "/boot/efi/"},
				{"/usr/lib/ostree-boot/efi/bcm2711-rpi-4-b.dtb", "/boot/efi/"},
				{"/usr/lib/ostree-boot/efi/bcm2711-rpi-400.dtb", "/boot/efi/"},
				{"/usr/lib/ostree-boot/efi/bcm2711-rpi-cm4.dtb", "/boot/efi/"},
				{"/usr/lib/ostree-boot/efi/bcm2711-rpi-cm4s.dtb", "/boot/efi/"},
				{"/usr/lib/ostree-boot/efi/bootcode.bin", "/boot/efi/"},
				{"/usr/lib/ostree-boot/efi/config.txt", "/boot/efi/config.txt"},
				{"/usr/lib/ostree-boot/efi/fixup.dat", "/boot/efi/"},
				{"/usr/lib/ostree-boot/efi/fixup4.dat", "/boot/efi/"},
				{"/usr/lib/ostree-boot/efi/fixup4cd.dat", "/boot/efi/"},
				{"/usr/lib/ostree-boot/efi/fixup4db.dat", "/boot/efi/"},
				{"/usr/lib/ostree-boot/efi/fixup4x.dat", "/boot/efi/"},
				{"/usr/lib/ostree-boot/efi/fixup_cd.dat", "/boot/efi/"},
				{"/usr/lib/ostree-boot/efi/fixup_db.dat", "/boot/efi/"},
				{"/usr/lib/ostree-boot/efi/fixup_x.dat", "/boot/efi/"},
				{"/usr/lib/ostree-boot/efi/overlays", "/boot/efi/"},
				{"/usr/share/uboot/rpi_arm64/u-boot.bin", "/boot/efi/rpi-u-boot.bin"},
				{"/usr/lib/ostree-boot/efi/start.elf", "/boot/efi/"},
				{"/usr/lib/ostree-boot/efi/start4.elf", "/boot/efi/"},
				{"/usr/lib/ostree-boot/efi/start4cd.elf", "/boot/efi/"},
				{"/usr/lib/ostree-boot/efi/start4db.elf", "/boot/efi/"},
				{"/usr/lib/ostree-boot/efi/start4x.elf", "/boot/efi/"},
				{"/usr/lib/ostree-boot/efi/start_cd.elf", "/boot/efi/"},
				{"/usr/lib/ostree-boot/efi/start_db.elf", "/boot/efi/"},
				{"/usr/lib/ostree-boot/efi/start_x.elf", "/boot/efi/"},
			},
		},
		iotRawImgType,
	)
	x86_64.addImageTypes(
		&platform.X86{
			UEFIVendor: "fedora",
			BasePlatform: platform.BasePlatform{
				ImageFormat: platform.FORMAT_RAW,
			},
		},
		minimalrawImgType,
	)
	aarch64.addImageTypes(
		&platform.Aarch64{
			UEFIVendor: "fedora",
			BasePlatform: platform.BasePlatform{
				ImageFormat: platform.FORMAT_RAW,
			},
		},
		minimalrawImgType,
	)

	rd.addArches(x86_64, aarch64)
	return &rd
}
