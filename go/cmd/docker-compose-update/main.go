package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/akamensky/argparse"
)

type programArgs struct {
	servicesDir *string
	verbose     *bool
}

const programDescription = `Script for updating multiple docker compose services. 
It requires that each service has its own directory which contains its docker-compose.yml file`

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

		dockerComposePath := filepath.Join(entry.Name(), "docker-compose.yml")
		if _, err := os.Stat(dockerComposePath); err == nil {
			dirs = append(dirs, entry.Name())
		}
	}

	return dirs, nil
}

func isServiceRunning(serviceDir string) (bool, error) {
	cmd := exec.Command("docker", "compose", "ps", "--services", "--status", "running")
	cmd.Dir = serviceDir

	output, err := cmd.Output()
	if err != nil {
		return false, err
	}

	// If the container is not running it will just return a newline ([10] as a byte array)
	if len(output) >= 2 {
		return true, nil
	}

	return false, nil
}

func updateService(serviceDir string) error {
	cmd := exec.Command("docker", "compose", "pull")
	cmd.Dir = serviceDir

	err := cmd.Run()
	if err != nil {
		return err
	}

	return nil
}

func startService(serviceDir string) error {
	cmd := exec.Command("docker", "compose", "up", "-d")
	cmd.Dir = serviceDir

	err := cmd.Run()
	if err != nil {
		return err
	}

	return nil
}

func main() {
	args := programArgs{}

	parser := argparse.NewParser("", programDescription)

	args.servicesDir = parser.String("d", "services-dir", &argparse.Options{Required: true, Help: "Path to directory which contains the services directories"})
	args.verbose = parser.Flag("v", "verbose", &argparse.Options{Help: "Enable verbose output"})

	err := parser.Parse(os.Args)
	if err != nil {
		fmt.Printf("Error: %v", parser.Usage(err))
		os.Exit(1)
	}

	if _, err := os.Stat(*args.servicesDir); err != nil {
		log.Fatal(err)
	}

	serviceDirs, err := findValidServiceDirs(*args.servicesDir)
	if err != nil {
		log.Fatal(err)
	}

	if *args.verbose {
		fmt.Printf("Found %v services\n", len(serviceDirs))
	}

	for _, serviceDir := range serviceDirs {
		serviceName := filepath.Base(serviceDir)

		running, err := isServiceRunning(serviceDir)
		if err != nil {
			fmt.Printf("Could not get docker status for %v: %v\n", serviceName, err)
			continue
		}

		if *args.verbose {
			fmt.Printf("Updating service %v...\n", serviceName)
		}
		err = updateService(serviceDir)
		if err != nil {
			fmt.Printf("Error updating service %v: %v\n", serviceName, err)
		}

		if running {
			fmt.Printf("Starting service %v...\n", serviceName)
			err = startService(serviceDir)
			if err != nil {
				fmt.Printf("Error starting service %v: %v\n", serviceName, err)
			}
		}
	}
}
