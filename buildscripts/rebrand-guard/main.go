// Copyright 2026 PGSTY contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

// rebrand-guard records the compatibility identifiers that a product rebrand
// must not accidentally rename. It intentionally excludes product branding and
// delivery names, which are validated by buildscripts/verify-rebrand.sh.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const manifestVersion = 4

var (
	minioImportRE = regexp.MustCompile(`github\.com/minio/[A-Za-z0-9_./-]+`)
	envRE         = regexp.MustCompile(`\b_?MINIO_[A-Z0-9_]+\b`)
	metricRE      = regexp.MustCompile(`\bminio_[A-Za-z0-9_]+\b`)
	headerRE      = regexp.MustCompile(`(?i)\bx-minio-[a-z0-9_-]+\b`)
	routeRE       = regexp.MustCompile(`^/[A-Za-z0-9._~!$&'()*+,;=:@%/?{}=-]*`)
	storageRE     = regexp.MustCompile(`\.minio\.sys(?:/[A-Za-z0-9._${}-]+)*`)
	policyRE      = regexp.MustCompile(`(?:arn:minio|minio:s3)[A-Za-z0-9_:/.*${}-]*`)
	brandRE       = regexp.MustCompile(`(?i)(^|[^a-z0-9_])minio([^a-z0-9_]|$)`)
)

type manifest struct {
	Version        int      `json:"version"`
	ModulePath     string   `json:"module_path"`
	MinioImports   []string `json:"minio_imports"`
	Environment    []string `json:"environment"`
	Metrics        []string `json:"metrics"`
	Headers        []string `json:"headers"`
	Routes         []string `json:"routes"`
	RouteRoots     []string `json:"route_roots"`
	GridRoutes     []string `json:"grid_routes"`
	StorageMarkers []string `json:"storage_markers"`
	PolicyValues   []string `json:"policy_values"`
	BrandAllowlist []string `json:"brand_allowlist"`
}

func main() {
	write := flag.Bool("write", false, "replace the checked-in compatibility baseline")
	flag.Parse()

	repo, err := gitOutput("rev-parse", "--show-toplevel")
	if err != nil {
		fatal(err)
	}
	repo = strings.TrimSpace(repo)
	baselinePath := filepath.Join(repo, "buildscripts", "rebrand-guard", "compat-baseline.json")

	current, err := collect(repo)
	if err != nil {
		fatal(err)
	}
	if *write {
		if err := writeManifest(baselinePath, current); err != nil {
			fatal(err)
		}
		fmt.Printf("wrote %s\n", baselinePath)
		printSummary(current)
		return
	}

	want, err := readManifest(baselinePath)
	if err != nil {
		fatal(err)
	}
	if err := compare(want, current); err != nil {
		fatal(err)
	}
	printSummary(current)
	fmt.Println("Silo rebrand compatibility baseline is unchanged")
}

func collect(repo string) (manifest, error) {
	files, err := trackedFiles(repo)
	if err != nil {
		return manifest{}, err
	}

	sets := map[string]map[string]struct{}{
		"imports": {},
		"env":     {},
		"metrics": {},
		"headers": {},
		"routes":  {},
		"roots":   {},
		"grid":    {},
		"storage": {},
		"policy":  {},
		"brand":   {},
	}
	modulePath := ""
	fset := token.NewFileSet()

	for _, rel := range files {
		if rel == "SILO_REBRANDING_MIGRATION.md" ||
			strings.HasPrefix(rel, "buildscripts/rebrand-guard/") ||
			strings.HasPrefix(rel, "buildscripts/helm-migration-guard/") {
			continue
		}
		path := filepath.Join(repo, filepath.FromSlash(rel))
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return manifest{}, fmt.Errorf("read %s: %w", rel, err)
		}
		if bytes.IndexByte(data, 0) >= 0 {
			continue
		}
		text := string(data)

		addMatches(sets["env"], envRE, text, false)
		addMatches(sets["headers"], headerRE, text, true)
		addMatches(sets["storage"], storageRE, text, false)
		addMatches(sets["policy"], policyRE, text, false)
		if strings.HasSuffix(rel, ".go") && (strings.HasPrefix(rel, "cmd/") || strings.HasPrefix(rel, "internal/")) {
			addMatches(sets["metrics"], metricRE, text, false)
		}
		if rel == "go.mod" {
			addMatches(sets["imports"], minioImportRE, text, false)
			for _, line := range strings.Split(text, "\n") {
				fields := strings.Fields(line)
				if len(fields) == 2 && fields[0] == "module" {
					modulePath = fields[1]
					break
				}
			}
		}
		if strings.HasSuffix(rel, ".go") {
			file, err := parser.ParseFile(fset, path, data, parser.SkipObjectResolution)
			if err != nil {
				return manifest{}, fmt.Errorf("parse %s: %w", rel, err)
			}
			for _, spec := range file.Imports {
				value, err := strconv.Unquote(spec.Path.Value)
				if err == nil && strings.HasPrefix(value, "github.com/minio/") {
					sets["imports"][value] = struct{}{}
				}
			}
			if !strings.HasSuffix(rel, "_test.go") {
				// Test files hold request paths for fixtures, not served routes.
				collectStringMatches(sets["routes"], routeRE, file)
				if strings.HasPrefix(rel, "cmd/") || strings.HasPrefix(rel, "internal/") {
					collectBrandStrings(sets["brand"], rel, file)
				}
			}
			collectNamedStringValues(sets["roots"], rel, file, "minioReservedBucket")
			if rel == "internal/grid/manager.go" {
				collectStringMatches(sets["grid"], routeRE, file)
			}
		}
	}
	// This was a shell-local PID variable in the generated inspect script,
	// never a supported environment setting.
	delete(sets["env"], "MINIO_SRVR_PID")

	if modulePath == "" {
		return manifest{}, errors.New("go.mod module path was not found")
	}
	return manifest{
		Version:        manifestVersion,
		ModulePath:     modulePath,
		MinioImports:   sorted(sets["imports"]),
		Environment:    sorted(sets["env"]),
		Metrics:        sorted(sets["metrics"]),
		Headers:        sorted(sets["headers"]),
		Routes:         sorted(sets["routes"]),
		RouteRoots:     sorted(sets["roots"]),
		GridRoutes:     sorted(sets["grid"]),
		StorageMarkers: sorted(sets["storage"]),
		PolicyValues:   sorted(sets["policy"]),
		BrandAllowlist: sorted(sets["brand"]),
	}, nil
}

func collectBrandStrings(dst map[string]struct{}, rel string, file *ast.File) {
	ast.Inspect(file, func(node ast.Node) bool {
		literal, ok := node.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return true
		}
		value, err := strconv.Unquote(literal.Value)
		if err != nil || !brandRE.MatchString(value) || strings.HasPrefix(value, "github.com/minio/") {
			return true
		}
		dst[filepath.ToSlash(rel)+"="+strconv.Quote(value)] = struct{}{}
		return true
	})
}

func collectNamedStringValues(dst map[string]struct{}, rel string, file *ast.File, names ...string) {
	wanted := make(map[string]struct{}, len(names))
	for _, name := range names {
		wanted[name] = struct{}{}
	}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, rawSpec := range gen.Specs {
			spec, ok := rawSpec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range spec.Names {
				if _, ok := wanted[name.Name]; !ok || i >= len(spec.Values) {
					continue
				}
				literal, ok := spec.Values[i].(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					continue
				}
				value, err := strconv.Unquote(literal.Value)
				if err == nil {
					dst[filepath.ToSlash(rel)+":"+name.Name+"="+value] = struct{}{}
				}
			}
		}
	}
}

func collectStringMatches(dst map[string]struct{}, re *regexp.Regexp, file *ast.File) {
	ast.Inspect(file, func(node ast.Node) bool {
		literal, ok := node.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return true
		}
		value, err := strconv.Unquote(literal.Value)
		if err == nil {
			addMatches(dst, re, value, false)
		}
		return true
	})
}

func trackedFiles(repo string) ([]string, error) {
	cmd := exec.Command("git", "-C", repo, "ls-files", "--cached", "-z")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}
	parts := bytes.Split(out, []byte{0})
	files := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) > 0 {
			files = append(files, string(part))
		}
	}
	return files, nil
}

func addMatches(dst map[string]struct{}, re *regexp.Regexp, text string, lower bool) {
	for _, match := range re.FindAllString(text, -1) {
		if lower {
			match = strings.ToLower(match)
		}
		dst[match] = struct{}{}
	}
}

func sorted(set map[string]struct{}) []string {
	values := make([]string, 0, len(set))
	for value := range set {
		values = append(values, value)
	}
	sort.Strings(values)
	return values
}

func writeManifest(path string, value manifest) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func readManifest(path string) (manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return manifest{}, fmt.Errorf("read compatibility baseline (run go run ./buildscripts/rebrand-guard --write once): %w", err)
	}
	var value manifest
	if err := json.Unmarshal(data, &value); err != nil {
		return manifest{}, err
	}
	if value.Version != manifestVersion {
		return manifest{}, fmt.Errorf("unsupported compatibility baseline version %d", value.Version)
	}
	return value, nil
}

func compare(want, got manifest) error {
	var failures []string
	if want.ModulePath != got.ModulePath {
		failures = append(failures, fmt.Sprintf("module_path: want %q, got %q", want.ModulePath, got.ModulePath))
	}
	checks := []struct {
		name      string
		want, got []string
	}{
		{"minio_imports", want.MinioImports, got.MinioImports},
		{"environment", want.Environment, got.Environment},
		{"metrics", want.Metrics, got.Metrics},
		{"headers", want.Headers, got.Headers},
		{"routes", want.Routes, got.Routes},
		{"route_roots", want.RouteRoots, got.RouteRoots},
		{"grid_routes", want.GridRoutes, got.GridRoutes},
		{"storage_markers", want.StorageMarkers, got.StorageMarkers},
		{"policy_values", want.PolicyValues, got.PolicyValues},
		{"brand_allowlist", want.BrandAllowlist, got.BrandAllowlist},
	}
	for _, check := range checks {
		if missing, added := setDiff(check.want, check.got); len(missing) > 0 || len(added) > 0 {
			var b strings.Builder
			fmt.Fprintf(&b, "%s compatibility set changed", check.name)
			for _, value := range missing {
				fmt.Fprintf(&b, "\n  - %s", value)
			}
			for _, value := range added {
				fmt.Fprintf(&b, "\n  + %s", value)
			}
			failures = append(failures, b.String())
		}
	}
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "\n"))
	}
	return nil
}

func setDiff(want, got []string) (missing, added []string) {
	wantSet := make(map[string]struct{}, len(want))
	gotSet := make(map[string]struct{}, len(got))
	for _, value := range want {
		wantSet[value] = struct{}{}
	}
	for _, value := range got {
		gotSet[value] = struct{}{}
	}
	for _, value := range want {
		if _, ok := gotSet[value]; !ok {
			missing = append(missing, value)
		}
	}
	for _, value := range got {
		if _, ok := wantSet[value]; !ok {
			added = append(added, value)
		}
	}
	return missing, added
}

func printSummary(value manifest) {
	fmt.Printf("compatibility manifest: imports=%d env=%d metrics=%d headers=%d routes=%d roots=%d grid=%d storage=%d policy=%d brand=%d sha256=%s\n",
		len(value.MinioImports), len(value.Environment), len(value.Metrics), len(value.Headers),
		len(value.Routes), len(value.RouteRoots), len(value.GridRoutes), len(value.StorageMarkers), len(value.PolicyValues),
		len(value.BrandAllowlist), manifestDigest(value))
}

func manifestDigest(value manifest) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func gitOutput(args ...string) (string, error) {
	out, err := exec.Command("git", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), strings.TrimSpace(string(out)), err)
	}
	return string(out), nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
