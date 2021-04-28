package iguazio

import "github.com/nuclio/nuclio/pkg/platform"

const (
	ProjectType = "project"
)

type Project struct {
	Data ProjectData `json:"data,omitempty"`
}

func (pl *Project) GetConfig() *platform.ProjectConfig {
	return &platform.ProjectConfig{
		Meta: platform.ProjectMeta{
			Name: pl.Data.Attributes.Name,
			Namespace: pl.Data.Attributes.Namespace,
			Annotations: pl.Data.Attributes.Annotations,
			Labels: pl.Data.Attributes.Labels,
		},
		Spec: platform.ProjectSpec{
			Description: pl.Data.Attributes.Description,
		},
	}
}

type ProjectData struct {
	Type       string            `json:"type,omitempty"`
	Attributes ProjectAttributes `json:"attributes,omitempty"`
}

type ProjectAttributes struct {
	Name          string            `json:"name,omitempty"`
	Namespace     string            `json:"namespace,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
	Annotations   map[string]string `json:"annotations,omitempty"`
	Description   string            `json:"description,omitempty"`
	NuclioProject NuclioProject     `json:"nuclio_project,omitempty"`
}

type NuclioProject struct {
	// currently no nuclio specific fields are needed
}

type ProjectList struct {
	Data []ProjectData `json:"data,omitempty"`
}

// ProjectList -> []Project
func (pl *ProjectList) ToSingleProjectList() []Project {
	var projects []Project

	for _, projectData := range pl.Data {
		projects = append(projects, Project{Data:projectData})
	}

	return projects
}
