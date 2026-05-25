package project

import (
	"encoding/xml"
	"os"
	"path/filepath"
)

// loadMaven resolves a maven project. If the pom at root has <module>
// children, each is loaded recursively (a child pom may itself have
// modules in a deep parent/aggregator layout).
//
// We use encoding/xml in lenient mode; pom.xml is well-formed XML and
// the only fields we care about are <modules>/<module>. We deliberately
// do not interpret inheritance or properties — fancy poms that compute
// module paths via properties (`<module>${child}</module>`) will look
// missing to us, and we just skip them.
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
		// Module declared by parent but child pom missing — skip
		// subtree but keep what we already added.
		return nil
	}
	var pm pomModules
	if err := xml.Unmarshal(b, &pm); err != nil {
		// Malformed pom is the user's problem; we shouldn't refuse to
		// detect the rest of the project because of one bad file.
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
