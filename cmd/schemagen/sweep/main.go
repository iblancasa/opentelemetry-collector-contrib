// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/open-telemetry/opentelemetry-collector-contrib/cmd/schemagen/internal"
	"gopkg.in/yaml.v3"
)

type stringList []string

func (s *stringList) String() string {
	return strings.Join(*s, ",")
}

func (s *stringList) Set(value string) error {
	if value == "" {
		return errors.New("empty value")
	}
	*s = append(*s, value)
	return nil
}

type Report struct {
	Issues         []internal.Issue `json:"issues" yaml:"issues"`
	MissingSchemas []string         `json:"missingSchemas" yaml:"missingSchemas"`
}

func main() {
	var roots stringList
	var reportPath string
	var fileType string
	var force bool

	flag.Var(&roots, "root", "Repository root to scan (repeatable)")
	flag.StringVar(&reportPath, "report", "", "Path to write issue report (json|yaml)")
	flag.StringVar(&fileType, "schema-type", "yaml", "Schema file type (yaml|json)")
	flag.BoolVar(&force, "force", false, "Regenerate schemas even if they already exist")
	flag.Parse()

	if len(roots) == 0 {
		fmt.Fprintln(os.Stderr, "at least one -root is required")
		flag.Usage()
		os.Exit(2)
	}

	report := Report{}
	for _, root := range roots {
		if err := sweepRoot(root, fileType, force, &report); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(1)
		}
	}

	if reportPath != "" {
		if err := writeReport(reportPath, report); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(1)
		}
	}
}

func sweepRoot(root, fileType string, force bool, report *Report) error {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	info, err := os.Stat(rootAbs)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("root %q is not a directory", rootAbs)
	}

	orig, _ := os.Getwd()
	_ = os.Chdir(rootAbs)
	settings, _ := internal.ReadSettingsFile()
	_ = os.Chdir(orig)

	err = filepath.WalkDir(rootAbs, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && shouldSkipDir(path) {
			return filepath.SkipDir
		}
		if d.IsDir() || d.Name() != "metadata.yaml" {
			return nil
		}
		dir := filepath.Dir(path)
		md, ok := internal.ReadMetadata(dir)
		if !ok {
			return nil
		}
		if !isComponentClass(md.Status.Class) {
			return nil
		}
		if !force && hasSchema(dir, md, settings) {
			return nil
		}
		cfg := buildConfig(dir, fileType, md, settings)
		parser := internal.NewParser(cfg)
		schema, perr := parser.Parse()
		if perr != nil {
			if errors.Is(perr, internal.ErrNoExportedTypes) {
				schema = internal.CreateSchema()
				schema.ObjectSchemaElement = *internal.CreateObjectField("")
			} else {
				report.MissingSchemas = append(report.MissingSchemas, dir)
				return nil
			}
		}
		if _, werr := internal.WriteSchemaToFile(schema, cfg); werr != nil {
			return werr
		}
		report.Issues = append(report.Issues, parser.Issues()...)
		return nil
	})

	return err
}

func buildConfig(dir, fileType string, md *internal.Metadata, settings *internal.Settings) *internal.Config {
	cfgType := "Config"
	class := md.Status.Class
	ctype := md.Type
	configPackage := ""
	configDir := ""
	mappings := internal.DefaultMappings()
	allowed := []string{}

	if settings != nil {
		mappings = internal.MergeMappings(internal.DefaultMappings(), settings.Mappings)
		comp := class + "/" + ctype
		if override, found := settings.ComponentOverrides[comp]; found {
			cfgType = override.ConfigName
			configDir = override.ConfigDir
		}
		allowed = settings.AllowedRefs
	}

	configNameParts := strings.Split(cfgType, ".")
	if len(configNameParts) == 2 {
		configPackage = configNameParts[0]
		cfgType = configNameParts[1]
	}
	if configDir == "" && configPackage != "" {
		configDir = configPackage
	}

	dirPath := dir
	outputFolder := dir
	if configDir != "" {
		if filepath.IsAbs(configDir) {
			dirPath = configDir
		} else {
			dirPath = filepath.Join(dir, configDir)
		}
		outputFolder = dirPath
	}

	return &internal.Config{
		Mode:          internal.Component,
		DirPath:       dirPath,
		OutputFolder:  outputFolder,
		ConfigPackage: configPackage,
		ConfigType:    cfgType,
		FileType:      fileType,
		Class:         class,
		Mappings:      mappings,
		AllowedRefs:   allowed,
	}
}

func hasSchema(dir string, md *internal.Metadata, settings *internal.Settings) bool {
	configDir := ""
	if settings != nil {
		comp := md.Status.Class + "/" + md.Type
		if override, found := settings.ComponentOverrides[comp]; found {
			configDir = override.ConfigDir
			if configDir == "" && override.ConfigName != "" {
				parts := strings.Split(override.ConfigName, ".")
				if len(parts) == 2 {
					configDir = parts[0]
				}
			}
		}
	}
	if configDir == "" {
		configDir = "config"
	}
	dirCandidates := []string{dir, filepath.Join(dir, configDir)}
	for _, name := range []string{"config.schema.json", "config.schema.yaml", "config.schema.yml"} {
		for _, base := range dirCandidates {
			if info, err := os.Stat(filepath.Join(base, name)); err == nil && !info.IsDir() {
				return true
			}
		}
	}
	return false
}

func shouldSkipDir(path string) bool {
	if strings.Contains(path, string(filepath.Separator)+"cmd"+string(filepath.Separator)+"mdatagen"+string(filepath.Separator)+"internal"+string(filepath.Separator)) {
		return true
	}
	return false
}

func isComponentClass(class string) bool {
	switch class {
	case "receiver", "processor", "exporter", "connector", "extension":
		return true
	default:
		return false
	}
}

func writeReport(path string, report Report) error {
	ext := strings.ToLower(filepath.Ext(path))
	var raw []byte
	var err error
	switch ext {
	case ".json":
		raw, err = json.MarshalIndent(report, "", "  ")
	case ".yaml", ".yml":
		raw, err = yaml.Marshal(report)
	default:
		return fmt.Errorf("unsupported report format: %q", ext)
	}
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}
