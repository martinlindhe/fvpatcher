package main

import (
	"crypto/md5"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/alecthomas/kong"
	"gopkg.in/yaml.v3"
)

var arg struct {
	EverquestRoot string `arg:"" help:"Root folder to patch." type:"existingdir"`
	Verbose       bool
	Expansion     string `required:"" enum:"original,kunark"`
	Client        string `required:"" enum:"rof"` // rof is for the rof2 client
}

func main() {
	kong.Parse(&arg)

	list, err := DownloadFileList(arg.Client, arg.Expansion)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Filelist manifest version", list.Version)
	list.HandleDeleteRequests(arg.EverquestRoot)
	list.HandleDownloadRequests(arg.EverquestRoot)
}

func (list *fileListYaml) HandleDeleteRequests(rootPath string) {
	fmt.Printf("Processing %d requests for deletes ...\n", len(list.Deletes))
	deleteCount := 0
	for _, del := range list.Deletes {
		fullPath := filepath.Join(rootPath, del.Name)
		if fileOrDirExists(fullPath) {
			fmt.Println("Deleting ", del.Name)
			err := os.Remove(fullPath)
			if err != nil {
				fmt.Println("- Delete failed:", err.Error())
			} else {
				deleteCount++
			}
		}
	}
	fmt.Printf("- %d files deleted\n", deleteCount)
}

func (list *fileListYaml) HandleDownloadRequests(rootPath string) {
	fmt.Printf("Processing %d requests for downloads ...\n", len(list.Downloads))
	downloadCount := 0
	for _, dl := range list.Downloads {
		fullPath := filepath.Join(rootPath, dl.Name)
		needDownload := false
		if fileOrDirExists(fullPath) {
			actualMD5, err := md5OfFile(fullPath)
			if err != nil {
				fmt.Println("ERROR:", err)
				continue
			}
			if actualMD5 != dl.MD5 {
				needDownload = true
			} else {
				if arg.Verbose {
					fmt.Println("OK", dl.Name)
				}
			}
		} else {
			needDownload = true
		}

		if needDownload {
			fileURL := list.DownloadPrefix + dl.Name
			fmt.Println("GET", fileURL)
			data, err := fetchUrl(fileURL)
			if err != nil {
				log.Fatal(err)
			}
			downloadedMD5 := md5OfData(data)
			if downloadedMD5 != dl.MD5 {
				fmt.Println("ERROR: Downloaded MD5 does not match. Got ", downloadedMD5, ", expected ", dl.MD5, ". Writing to disk anyway!!!")
				if err := writeFile(fullPath, data); err != nil {
					log.Println("ERROR:", err)
				}
				downloadCount++
			}
		}
	}
	fmt.Printf("- %d files downloaded\n", downloadCount)
}

func DownloadFileList(clientName, expansion string) (*fileListYaml, error) {

	filelistName := "filelist_" + arg.Client + "." + arg.Expansion + ".yml"
	filelistFullPath := filepath.Join(getSettingsRoot(), filelistName)
	filelistURL := "https://" + arg.Expansion + ".fvproject.com/" + arg.Client + "/filelist_" + arg.Client + ".yml"

	fmt.Println("Filelist URL is", filelistURL)

	if !fileOrDirExists(filelistFullPath) || isCachedFileTooOld(filelistFullPath, 7) {
		fmt.Println("GET", filelistURL, "...")
		data, err := fetchUrl(filelistURL)
		if err != nil {
			return nil, err
		}
		if err := os.MkdirAll(getSettingsRoot(), 0777); err != nil {
			return nil, err
		}
		if err := writeFile(filelistFullPath, data); err != nil {
			return nil, err
		}
	}

	data, err := os.ReadFile(filelistName)
	if err != nil {
		return nil, err
	}

	var list fileListYaml
	err = yaml.Unmarshal(data, &list)
	return &list, err
}

func md5OfData(data []byte) string {
	return fmt.Sprintf("%x", md5.Sum(data))
}

func md5OfFile(fileName string) (string, error) {
	f, err := os.Open(fileName)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

type fileListYaml struct {
	Version        string
	Deletes        []fileEntry
	DownloadPrefix string
	Downloads      []fileEntry
}

type fileEntry struct {
	Name string
	MD5  string
	Date string
	Size uint
}

func getSettingsRoot() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".config/fvpatcher")
}

func fileOrDirExists(path string) bool {
	if _, err := os.Stat(path); err != nil && os.IsNotExist(err) {
		return false
	}
	return true
}

func writeFile(fileName string, data []byte) error {
	f, err := os.Create(fileName)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(data)
	return err
}

func fetchUrl(url string) ([]byte, error) {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{
		Timeout:   30 * time.Second,
		Transport: tr}

	response, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	return io.ReadAll(response.Body)
}

func isCachedFileTooOld(fileName string, maxDays int) bool {
	info, err := os.Stat(fileName)
	if err != nil {
		return true
	}
	mod := info.ModTime()
	maxAge := time.Now().AddDate(0, 0, -maxDays)
	return mod.Before(maxAge)
}
