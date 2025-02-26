package fdo

import "github.com/osbuild/osbuild-composer/internal/blueprint"

type Options struct {
	ManufacturingServerURL string
	DiunPubKeyInsecure     string
	DiunPubKeyHash         string
	DiunPubKeyRootCerts    string
}

func FromBP(bpFDO blueprint.FDOCustomization) *Options {
	fdo := Options(bpFDO)
	return &fdo
}
