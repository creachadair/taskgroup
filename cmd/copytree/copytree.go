// Binary copytree is an example program to demonstrate the use of the group
// and throttle packages to manage concurrency.  It recursively copies a tree
// of files from one directory to another.
//
// Usage:
//    copytree -from /path/to/source -to /path/to/target
//
package main

import (
	"flag"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"bitbucket.org/creachadair/group"
	"bitbucket.org/creachadair/group/throttle"
	"golang.org/x/net/context"
)

var (
	srcPath    = flag.String("from", "", "Source path (required)")
	dstPath    = flag.String("to", "", "Destination path (required)")
	maxWorkers = flag.Int("workers", 1, "Maximum number of concurrent tasks")
)

func main() {
	flag.Parse()

	if *srcPath == "" || *dstPath == "" {
		log.Fatal("You must provide both --from and --to paths")
	}
	var destExists bool
	if _, err := os.Stat(*dstPath); err == nil {
		destExists = true
	}

	g := throttle.Capacity(group.New(context.Background()), *maxWorkers)
	err := filepath.Walk(*srcPath, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		target := adjustPath(path)
		if fi.IsDir() {
			return os.MkdirAll(target, fi.Mode())
		}
		return g.Go(func(ctx context.Context) error {
			log.Printf("Copying %q", path)
			return copyFile(path, target)
		})
	})
	if err != nil {
		log.Printf("Error traversing directory: %v", err)
		g.Cancel()
	}
	if err := g.Wait(); err != nil {
		log.Printf("Error copying: %v", err)
		if !destExists {
			log.Printf("Cleaning up %q...", *dstPath)
			os.RemoveAll(*dstPath)
		}
		os.Exit(1)
	}
}

// adjustPath modifies path to be relative to the destination by stripping off
// the source prefix and conjoining it with the destination path.
func adjustPath(path string) string {
	return filepath.Join(*dstPath, strings.TrimPrefix(path, *srcPath))
}

// copyFile copies a plain file from source to target.
func copyFile(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(target)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
