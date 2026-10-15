package project

import (
	"encoding/xml"
	"os"
	"path/filepath"
)

func loadMaven(root string) (*Project, error) {
	p := &Project{Root: root, Kind: KindMaven}
	if err := addMavenModule(p, root, ""); err != nil {
		return nil, err
	}
	return p, nil
}

type pomModules struct {
	XMLName xml.Name `xml:"project"`
	Modules struct {
		Module []string `xml:"module"`
	} `xml:"modules"`
}

func addMavenModule(p *Project, dir string, name string) error {
	mod := &Module{
		Name:        name,
		Path:        dir,
		SourceRoots: standardSourceRoots(dir),
	}
	p.Modules = append(p.Modules, mod)

	pomPath := filepath.Join(dir, "pom.xml")
	b, err := os.ReadFile(pomPath)
	if err != nil {
		return nil
	}
	var pm pomModules
	if err := xml.Unmarshal(b, &pm); err != nil {
		return nil
	}
	for _, sub := range pm.Modules.Module {
		if sub == "" {
			continue
		}
		subPath := filepath.Join(dir, sub)
		if _, err := os.Stat(subPath); err != nil {
			continue
		}
		subName := sub
		if name != "" {
			subName = name + "/" + sub
		}
		if err := addMavenModule(p, subPath, subName); err != nil {
			return err
		}
	}
	return nil
}
