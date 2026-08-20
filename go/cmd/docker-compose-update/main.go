package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mihmin98/scripts/pkg/argparse"
	"github.com/schollz/progressbar/v3"
)

type UpdateResult struct {
	ServiceName string
	Status      string // "updated", "up-to-date", "failed"
	Error       error
	OldImage    string
	NewImage    string
}

type programArgs struct {
	servicesDir string
	verbose     bool
	dryRun      bool
	timeout     time.Duration
}

// duration parses a Go duration such as "5m" or "30s".
var duration = argparse.NewType("duration", func(s string) (any, error) {
	return time.ParseDuration(s)
})

const programDescription = `Script for updating multiple docker compose services.
It requires that each service has its own directory which contains its docker-compose.yml file`

func getCurrentImages(serviceDir string) (map[string]string, error) {
	cmd := exec.Command("docker", "compose", "ps", "--format", "json")
	cmd.Dir = serviceDir

	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	images := make(map[string]string)
	for _, line := range strings.Split(string(output), "\n") {
		if line == "" {
			continue
		}
		var entry struct {
			Image string `json:"Image"`
			Name  string `json:"Name"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.Image != "" {
			images[entry.Name] = entry.Image
		}
	}

	return images, nil
}

func findValidServiceDirs(servicesDir string) ([]string, error) {
	var dirs []string

	entries, err := os.ReadDir(servicesDir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		fullServiceDir := filepath.Join(servicesDir, entry.Name())
		dockerComposePath := filepath.Join(fullServiceDir, "docker-compose.yml")
		if _, err := os.Stat(dockerComposePath); err == nil {
			dirs = append(dirs, fullServiceDir)
		}
	}

	return dirs, nil
}

func isServiceRunning(serviceDir string) (bool, error) {
	cmd := exec.Command("docker", "compose", "ps", "--services", "--status", "running")
	cmd.Dir = serviceDir

	output, err := cmd.Output()
	if err != nil {
		// Exit code 1 means no running containers - this is normal
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, err
	}

	return len(strings.TrimSpace(string(output))) > 0, nil
}

func updateService(serviceDir string, verbose bool) (*UpdateResult, error) {
	serviceName := filepath.Base(serviceDir)
	result := &UpdateResult{ServiceName: serviceName}

	// Get current images before pull
	oldImages, err := getCurrentImages(serviceDir)
	if err != nil && verbose {
		fmt.Fprintf(os.Stderr, "Warning: could not get current images for %s: %v\n", serviceName, err)
	}

	// Run docker compose up -d --pull always (combines pull and start)
	cmd := exec.Command("docker", "compose", "up", "-d", "--pull", "always")
	cmd.Dir = serviceDir

	if verbose {
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
	}

	err = cmd.Run()
	if err != nil {
		result.Status = "failed"
		result.Error = err
		return result, err
	}

	// Get images after pull
	newImages, err := getCurrentImages(serviceDir)
	if err != nil && verbose {
		fmt.Fprintf(os.Stderr, "Warning: could not get new images for %s: %v\n", serviceName, err)
	}

	// Compare to determine if anything actually changed
	// Check if any service image changed
	updated := false
	for name, newImg := range newImages {
		if oldImg, ok := oldImages[name]; ok && oldImg != newImg {
			if !updated {
				result.OldImage = oldImg
				updated = true
			}
			result.NewImage = newImg
		}
	}

	if updated {
		result.Status = "updated"
	} else {
		result.Status = "up-to-date"
	}

	return result, nil
}

func main() {
	args := programArgs{}

	parser := argparse.NewParser("", programDescription)

	parser.MustAddArgument([]string{"-d", "--services-dir"}, &argparse.Options{Required: true, Help: "Path to directory which contains the services directories"})
	parser.MustAddArgument([]string{"-v", "--verbose"}, &argparse.Options{Action: argparse.StoreTrue, Help: "Enable verbose output"})
	parser.MustAddArgument([]string{"-n", "--dry-run"}, &argparse.Options{Action: argparse.StoreTrue, Help: "Print what would be done without executing"})
	parser.MustAddArgument([]string{"-t", "--timeout"}, &argparse.Options{Type: duration, Help: "Timeout for each service update (e.g., 5m, 30s)"})

	ns := parser.MustParseArgs(os.Args[1:])
	args.servicesDir = ns.String("services_dir")
	args.verbose = ns.Bool("verbose")
	args.dryRun = ns.Bool("dry_run")
	if v, ok := ns.Get("timeout"); ok && v != nil {
		args.timeout = v.(time.Duration)
	}

	servicesDir, err := filepath.Abs(args.servicesDir)
	if err != nil {
		parser.Fail(err.Error())
	}

	if _, err := os.Stat(servicesDir); err != nil {
		log.Fatal(err)
	}

	serviceDirs, err := findValidServiceDirs(servicesDir)
	if err != nil {
		log.Fatal(err)
	}

	if args.verbose {
		fmt.Printf("Found %v services\n", len(serviceDirs))
	}

	// Set up timeout context
	ctx := context.Background()
	var cancel context.CancelFunc
	if args.timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, args.timeout)
		defer cancel()
	}

	// Set up progress bar
	bar := progressbar.Default(int64(len(serviceDirs)))

	// Process services (sequentially, no parallel processing)
	var updatedCount, upToDateCount, failedCount int
	var updatedServices, failedServices []string

	for _, serviceDir := range serviceDirs {
		select {
		case <-ctx.Done():
			log.Fatalf("Timeout exceeded: %v", ctx.Err())
		default:
		}

		serviceName := filepath.Base(serviceDir)

		if args.verbose {
			fmt.Printf("\nUpdating service %v...\n", serviceName)
		}

		var result *UpdateResult
		if args.dryRun {
			// In dry-run mode, just check what images are available
			result = &UpdateResult{ServiceName: serviceName}
			oldImages, _ := getCurrentImages(serviceDir)
			newImages, _ := getCurrentImages(serviceDir)

			updated := false
			for name, newImg := range newImages {
				if oldImg, ok := oldImages[name]; ok && oldImg != newImg {
					result.OldImage = oldImg
					result.NewImage = newImg
					updated = true
				}
			}
			if updated {
				result.Status = "updated"
			} else {
				result.Status = "up-to-date"
			}
		} else {
			result, _ = updateService(serviceDir, args.verbose)
		}

		if args.verbose {
			switch result.Status {
			case "updated":
				fmt.Printf("[✓] %s: updated %s → %s\n", result.ServiceName, result.OldImage, result.NewImage)
			case "up-to-date":
				fmt.Printf("[ ] %s: up-to-date\n", result.ServiceName)
			case "failed":
				fmt.Printf("[✗] %s: failed (%v)\n", result.ServiceName, result.Error)
			}
		}

		bar.Add(1)

		switch result.Status {
		case "updated":
			updatedCount++
			updatedServices = append(updatedServices, result.ServiceName)
		case "up-to-date":
			upToDateCount++
		case "failed":
			failedCount++
			failedServices = append(failedServices, result.ServiceName)
		}
	}

	// Print summary
	if !args.verbose {
		fmt.Printf("Summary:\n")
		fmt.Printf("  Total:     %d\n", updatedCount+upToDateCount+failedCount)
		fmt.Printf("  Updated:   %d\n", updatedCount)
		fmt.Printf("  Up-to-date: %d\n", upToDateCount)
		fmt.Printf("  Failed:    %d\n", failedCount)

		if len(updatedServices) > 0 {
			fmt.Printf("\nUpdated services:\n")
			for _, name := range updatedServices {
				fmt.Printf("  - %s\n", name)
			}
		}

		if len(failedServices) > 0 {
			fmt.Printf("\nFailed services:\n")
			for _, name := range failedServices {
				fmt.Printf("  - %s\n", name)
			}
		}
	}

	// Exit with error code if any failures
	if failedCount > 0 {
		os.Exit(1)
	}
}
