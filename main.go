package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/rpajarola/dedup/fingerprint"
)

func processDirectory(path string) error {
	return filepath.Walk(path, func(filename string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		fps, err := fingerprint.GetFingerprint(filename)
		if err != nil {
			log.Printf("Error processing %v: %v", filename, err)
			return nil // Continue with next file
		}

		printFPs(filename, fps)
		return nil
	})
}

func printFPs(filename string, fps []fingerprint.Fingerprint) {
	fmt.Printf("%v:\n", filename)
	for _, fp := range fps {
		fmt.Printf("  %v: %v\n", fp.Kind, fp.Hash)
	}
}

func main() {
	var imageDir string
	if len(os.Args) != 2 {
		imageDir = "testdata"
		//log.Fatal("Usage: program <image_directory>")
	} else {
		imageDir = os.Args[1]
	}
	if err := processDirectory(imageDir); err != nil {
		log.Fatal(err)
	}
}
