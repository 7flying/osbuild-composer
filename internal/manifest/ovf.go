package manifest

import (
	"fmt"

	"github.com/osbuild/osbuild-composer/internal/osbuild"
)

// A OVF copies a vmdk image to it's own tree and generates an OVF descriptor
type OVF struct {
	Base

	imgPipeline *VMDK
}

// NewOVF creates a new OVF pipeline. imgPipeline is the pipeline producing the vmdk image.
func NewOVF(m *Manifest,
	buildPipeline *Build,
	imgPipeline *VMDK) *OVF {
	p := &OVF{
		Base:        NewBase(m, "ovf", buildPipeline),
		imgPipeline: imgPipeline,
	}
	if imgPipeline.Base.manifest != m {
		panic("live image pipeline from different manifest")
	}
	buildPipeline.addDependent(p)
	m.addPipeline(p)
	return p
}

func (p *OVF) serialize() osbuild.Pipeline {
	pipeline := p.Base.serialize()

	inputName := "vmdk-tree"
	pipeline.AddStage(osbuild.NewCopyStageSimple(
		&osbuild.CopyStageOptions{
			Paths: []osbuild.CopyStagePath{
				osbuild.CopyStagePath{
					From: fmt.Sprintf("input://%s/%s", inputName, p.imgPipeline.Export().Filename()),
					To:   "tree:///",
				},
			},
		},
		osbuild.NewPipelineTreeInputs(inputName, p.imgPipeline.Name()),
	))

	pipeline.AddStage(osbuild.NewOVFStage(&osbuild.OVFStageOptions{
		Vmdk: p.imgPipeline.Filename,
	}))

	return pipeline
}

func (p *OVF) getBuildPackages() []string {
	return []string{"qemu-img"}
}
