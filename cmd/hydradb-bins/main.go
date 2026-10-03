package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/oci"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
)

const (
	layerMediaType    = "application/vnd.oci.image.layer.v1.tar+gzip"
	artifactType      = "application/vnd.hydradb.payload.v1"
	dockerIndexType   = "application/vnd.docker.distribution.manifest.list.v2+json"
	layerMode         = 0o644
	layoutMarkerName  = "oci-layout"
	defaultImage      = "ghcr.io/hydra-db/hydradb"
	defaultTag        = "0.2.0"
	defaultPlatform   = "linux/amd64"
	defaultOutputDir  = "hydradb-binaries"
	artifactTagSuffix = "hydradb-binaries"
)

type payloadFile struct {
	source string
	target string
}

var payloadFiles = []payloadFile{
	{source: "usr/local/bin/graph-node", target: "graph-node"},
	{source: "usr/local/bin/graph-indexer", target: "graph-indexer"},
	{source: "usr/lib/x86_64-linux-gnu/libgraphblas.so.7.4.0", target: "lib/libgraphblas.so.7.4.0"},
}

func main() {
	image := flag.String("image", defaultImage, "source image repository")
	tag := flag.String("tag", defaultTag, "source image tag")
	platform := flag.String("platform", defaultPlatform, "source image platform as os/arch")
	out := flag.String("out", defaultOutputDir, "destination OCI image layout directory")
	artifactTag := flag.String("artifact-tag", "", "artifact tag inside the layout")
	flag.Parse()

	if err := run(context.Background(), *image, *tag, *platform, *out, *artifactTag); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, image, tag, platform, out, artifactTag string) error {
	repo, err := remote.NewRepository(image)
	if err != nil {
		return err
	}
	repo.Client = auth.DefaultClient

	resolved, err := repo.Resolve(ctx, tag)
	if err != nil {
		return fmt.Errorf("resolve %s:%s: %w", image, tag, err)
	}

	manifestDesc := resolved
	if isIndex(resolved.MediaType) {
		manifestDesc, err = selectPlatform(ctx, repo, resolved, platform)
		if err != nil {
			return err
		}
	}

	manifest, err := fetchManifest(ctx, repo, manifestDesc)
	if err != nil {
		return err
	}

	files, err := extract(ctx, repo, manifest.Layers)
	if err != nil {
		return err
	}

	if artifactTag == "" {
		artifactTag = fmt.Sprintf("%s:%s", artifactTagSuffix, tag)
	}

	return pack(ctx, out, artifactTag, files)
}

func isIndex(mediaType string) bool {
	return mediaType == ocispec.MediaTypeImageIndex || mediaType == dockerIndexType
}

func selectPlatform(ctx context.Context, repo *remote.Repository, desc ocispec.Descriptor, platform string) (ocispec.Descriptor, error) {
	parts := strings.SplitN(platform, "/", 2)
	if len(parts) != 2 {
		return ocispec.Descriptor{}, fmt.Errorf("platform must be os/arch, got %q", platform)
	}

	raw, err := content.FetchAll(ctx, repo, desc)
	if err != nil {
		return ocispec.Descriptor{}, err
	}

	var index ocispec.Index
	if err := json.Unmarshal(raw, &index); err != nil {
		return ocispec.Descriptor{}, err
	}

	for _, candidate := range index.Manifests {
		if candidate.Platform == nil {
			continue
		}
		if candidate.Platform.OS == parts[0] && candidate.Platform.Architecture == parts[1] {
			return candidate, nil
		}
	}

	return ocispec.Descriptor{}, fmt.Errorf("platform %s not present in index", platform)
}

func fetchManifest(ctx context.Context, repo *remote.Repository, desc ocispec.Descriptor) (ocispec.Manifest, error) {
	raw, err := content.FetchAll(ctx, repo, desc)
	if err != nil {
		return ocispec.Manifest{}, err
	}

	var manifest ocispec.Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return ocispec.Manifest{}, err
	}

	return manifest, nil
}

func extract(ctx context.Context, repo *remote.Repository, layers []ocispec.Descriptor) (map[string][]byte, error) {
	wanted := make(map[string]string, len(payloadFiles))
	for _, file := range payloadFiles {
		wanted[file.source] = file.target
	}

	found := make(map[string][]byte, len(payloadFiles))
	for _, layer := range layers {
		if err := scanLayer(ctx, repo, layer, wanted, found); err != nil {
			return nil, err
		}
	}

	for _, file := range payloadFiles {
		if _, ok := found[file.target]; !ok {
			return nil, fmt.Errorf("source image did not provide %s", file.source)
		}
	}

	return found, nil
}

func scanLayer(ctx context.Context, repo *remote.Repository, layer ocispec.Descriptor, wanted map[string]string, found map[string][]byte) error {
	rc, err := repo.Fetch(ctx, layer)
	if err != nil {
		return err
	}
	defer rc.Close()

	reader, err := decompress(rc, layer.MediaType)
	if err != nil {
		return err
	}
	if closer, ok := reader.(io.Closer); ok {
		defer closer.Close()
	}

	tr := tar.NewReader(reader)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}

		target, ok := wanted[normalize(header.Name)]
		if !ok {
			continue
		}

		body, err := io.ReadAll(tr)
		if err != nil {
			return err
		}
		found[target] = body
	}
}

func decompress(reader io.Reader, mediaType string) (io.Reader, error) {
	if strings.Contains(mediaType, "gzip") {
		return gzip.NewReader(reader)
	}
	return reader, nil
}

func normalize(name string) string {
	return strings.TrimPrefix(path.Clean("/"+name), "/")
}

func pack(ctx context.Context, out, artifactTag string, files map[string][]byte) error {
	if err := resetLayout(out); err != nil {
		return err
	}

	store, err := oci.New(out)
	if err != nil {
		return err
	}

	layer, err := buildLayer(files)
	if err != nil {
		return err
	}

	layerDesc := content.NewDescriptorFromBytes(layerMediaType, layer)
	if err := store.Push(ctx, layerDesc, bytes.NewReader(layer)); err != nil {
		return err
	}

	manifestDesc, err := oras.PackManifest(
		ctx,
		store,
		oras.PackManifestVersion1_1,
		artifactType,
		oras.PackManifestOptions{Layers: []ocispec.Descriptor{layerDesc}},
	)
	if err != nil {
		return err
	}

	if err := store.Tag(ctx, manifestDesc, artifactTag); err != nil {
		return err
	}

	fmt.Printf("artifact: %s\n", artifactTag)
	fmt.Printf("layout:   %s\n", out)
	fmt.Printf("digest:   %s\n", manifestDesc.Digest)

	for _, file := range payloadFiles {
		fmt.Printf("file:     %s (%d bytes)\n", file.target, len(files[file.target]))
	}

	return nil
}

func resetLayout(dir string) error {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil
	}
	if _, err := os.Stat(filepath.Join(dir, layoutMarkerName)); err != nil {
		return fmt.Errorf("%s exists but is not an OCI image layout; refusing to remove it", dir)
	}
	return os.RemoveAll(dir)
}

func buildLayer(files map[string][]byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)

	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		body := files[name]
		header := &tar.Header{Name: name, Mode: layerMode, Size: int64(len(body))}
		if err := tw.WriteHeader(header); err != nil {
			return nil, err
		}
		if _, err := tw.Write(body); err != nil {
			return nil, err
		}
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
