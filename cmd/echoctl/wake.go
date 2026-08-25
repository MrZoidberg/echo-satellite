package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/MrZoidberg/echo-satellite/internal/device/wake"
)

func wakeInstall(w io.Writer, command wakeInstallCommand) error {
	store := wake.Store{Root: command.ModelDir}
	model, err := store.Install(wake.InstallRequest{
		ID: command.Args.ID, SourcePath: command.From, SidecarPath: command.Metadata,
		ExpectedSHA256: command.SHA256, Overwrite: command.Overwrite,
	})
	if err != nil {
		return fmt.Errorf("install wake model: %w", err)
	}
	return writeReport(w, []string{
		"installed: " + model.ID,
		fmt.Sprintf("kind:      %s", model.Kind),
		"sha256:    " + model.SHA256,
	})
}

func wakeList(w io.Writer, command wakeListCommand) error {
	store := wake.Store{Root: command.ModelDir}
	models, err := store.List()
	if err != nil {
		return fmt.Errorf("list wake models: %w", err)
	}
	sharedMel := filePresent(store.SharedPath("melspectrogram.tflite"))
	sharedEmbedding := filePresent(store.SharedPath("embedding_model.tflite"))
	lines := []string{fmt.Sprintf("shared: mel=%t embedding=%t", sharedMel, sharedEmbedding)}
	if len(models) == 0 {
		lines = append(lines, "models: none")
	}
	for _, model := range models {
		digest := model.SHA256
		if len(digest) > 12 {
			digest = digest[:12]
		}
		languages := strings.Join(model.Languages, ",")
		if languages == "" {
			languages = "n/a"
		}
		lines = append(lines, fmt.Sprintf(
			"model: %s kind=%s phrase=%q languages=%s size=%d sha256=%s",
			model.ID, model.Kind, model.Phrase, languages, model.Size, digest,
		))
	}
	return writeReport(w, lines)
}

func filePresent(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
