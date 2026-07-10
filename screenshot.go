package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/chromedp"
)

// fileURL turns an absolute filesystem path into a file:// URL that works on
// all platforms. On Windows filepath.ToSlash turns C:\a\b into C:/a/b, and the
// leading slash makes it file:///C:/a/b; on unix /a/b already starts with a
// slash, giving file:///a/b.
func fileURL(p string) string {
	p = filepath.ToSlash(p)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return "file://" + p
}

// Heavily inspired by: https://github.com/chromedp/examples/blob/master/download_file/main.go
func screenshot(fileURL, format, fileName string) {
	ctx, cancel := chromedp.NewContext(context.Background())
	defer cancel()

	// create a timeout as a safety net to prevent any infinite wait loops
	ctx, cancel = context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	// Which export button to drive, and the resulting file extension.
	clickSelector := "#saveGraph"
	ext := "svg"
	if format == "png" {
		clickSelector = "#savePng"
		ext = "png"
	}

	// this will be used to capture the file name later
	var downloadGUID string

	downloadComplete := make(chan bool)
	chromedp.ListenTarget(ctx, func(v interface{}) {
		if ev, ok := v.(*browser.EventDownloadProgress); ok {
			if ev.State == browser.DownloadProgressStateCompleted {
				downloadGUID = ev.GUID
				close(downloadComplete)
			}
		}
	})

	if err := chromedp.Run(ctx, chromedp.Tasks{
		browser.SetDownloadBehavior(browser.SetDownloadBehaviorBehaviorAllowAndName).
			WithDownloadPath(os.TempDir()).
			WithEventsEnabled(true),

		chromedp.Navigate(fileURL),
		// Wait for the graph container to mount, then let the klay layout settle
		// before exporting so the SVG/PNG captures the laid-out graph, not an empty
		// one. Use WaitReady, not WaitVisible: the cytoscape container renders its
		// graph on absolutely-positioned canvases and reports no box model of its
		// own to a GPU-less headless Chrome, so WaitVisible never returns.
		chromedp.WaitReady(`#cytoscape-div`),
		chromedp.Sleep(2 * time.Second),
		// the export buttons live in a dropdown, so open it first, then click the
		// target (#saveGraph for SVG, #savePng for PNG).
		chromedp.Click(`#exportToggle`, chromedp.NodeVisible),
		chromedp.WaitVisible(clickSelector),
		chromedp.Click(clickSelector, chromedp.NodeVisible),
	}); err != nil && !strings.Contains(err.Error(), "net::ERR_ABORTED") {
		// Note: Ignoring the net::ERR_ABORTED page error is essential here since downloads
		// will cause this error to be emitted, although the download will still succeed.
		log.Fatal(err)
	}
	// Don't block forever: if the download never fires (e.g. the export failed)
	// the context timeout needs to be able to kill us rather than hang.
	select {
	case <-downloadComplete:
	case <-ctx.Done():
		log.Fatalf("Timed out waiting for the %s export to download", ext)
	}

	dest := fmt.Sprintf("./%s.%s", fileName, ext)
	e := moveFile(fmt.Sprintf("%v/%v", os.TempDir(), downloadGUID), dest)
	if e != nil {
		log.Fatal(e)
	}

	log.Printf("Image generation complete: %s", dest)
}

// This function resolves the "invalid cross-device link" error for moving files
// between volumes for Docker.
// https://gist.github.com/var23rav/23ae5d0d4d830aff886c3c970b8f6c6b
func moveFile(sourcePath, destPath string) error {
	inputFile, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("Couldn't open source file: %s", err)
	}
	outputFile, err := os.Create(destPath)
	if err != nil {
		inputFile.Close()
		return fmt.Errorf("Couldn't open dest file: %s", err)
	}
	defer outputFile.Close()
	_, err = io.Copy(outputFile, inputFile)
	inputFile.Close()
	if err != nil {
		return fmt.Errorf("Writing to output file failed: %s", err)
	}
	// The copy was successful, so now delete the original file
	err = os.Remove(sourcePath)
	if err != nil {
		return fmt.Errorf("Failed removing original file: %s", err)
	}
	return nil
}
