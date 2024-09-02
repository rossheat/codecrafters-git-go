package main

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: mygit <command> [<args>...]\n")
		os.Exit(1)
	}

	switch command := os.Args[1]; command {
	case "init":
		initRepository()
	case "cat-file":
		if len(os.Args) < 4 {
			fmt.Fprintf(os.Stderr, "usage: cat-file -p <blob_sha>\n")
			os.Exit(1)
		}
		catFile(os.Args[3])
	case "hash-object":
		if len(os.Args) < 4 {
			fmt.Fprintf(os.Stderr, "usage: hash-object <filename>\n")
			os.Exit(1)
		}
		hashObject(os.Args[3])
	case "ls-tree":
		if len(os.Args) < 4 {
			fmt.Fprintf(os.Stderr, "usage: ls-tree <tree_sha>\n")
			os.Exit(1)
		}
		lsTree(os.Args[3])
	case "write-tree":
		writeTree()
	case "commit-tree":
		commitTree()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command %s\n", command)
		os.Exit(1)
	}
}

func initRepository() {
	for _, dir := range []string{".git", ".git/objects", ".git/refs"} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "Error creating directory: %s\n", err)
		}
	}
	headFileContents := []byte("ref: refs/heads/main\n")
	if err := os.WriteFile(".git/HEAD", headFileContents, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing file: %s\n", err)
	}
	fmt.Println("Initialized git directory")
}

func catFile(blobSHA string) {
	blobPath := filepath.Join(".git/objects", blobSHA[:2], blobSHA[2:])
	content := readAndDecompressFile(blobPath)
	blobContent := strings.SplitN(content, "\x00", 2)[1]
	fmt.Print(blobContent)
}

func hashObject(filename string) {
	fileBytes, err := os.ReadFile(filename)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file %v: %v", filename, err.Error())
		os.Exit(1)
	}
	hash := createObject("blob", fileBytes)
	fmt.Println(hex.EncodeToString(hash))
}

func lsTree(treeSHA string) {
	treeFilePath := filepath.Join(".git/objects", treeSHA[:2], treeSHA[2:])
	content := readAndDecompressFile(treeFilePath)
	modes := []string{"100644", "100755", "120000", "40000"}
	re := regexp.MustCompile(`[\x00\s]`)
	contentParts := re.Split(content, -1)

	filesAndDirs := make([]string, 0)
	for index := 0; index < len(contentParts); index++ {
		for _, mode := range modes {
			if strings.Contains(contentParts[index], mode) {
				filesAndDirs = append(filesAndDirs, contentParts[index+1])
			}
		}
	}
	fmt.Println(strings.Join(filesAndDirs, "\n"))
}

func writeTree() {
	treeObjectHash := createTreeObjects(".")
	fmt.Println(hex.EncodeToString(treeObjectHash))
}

type HashedEntry struct {
	mode string
	name string
	hash []byte
}

func createTreeObjects(path string) []byte {
	hashedEntries := make([]HashedEntry, 0)
	entries, _ := os.ReadDir(path)

	sort.Slice(entries, func(i, j int) bool {
		return strings.ToLower(entries[i].Name()) < strings.ToLower(entries[j].Name())
	})

	for _, entry := range entries {
		entryPath := filepath.Join(path, entry.Name())
		if entry.IsDir() {
			if entry.Name() == ".git" {
				continue
			}
			he := HashedEntry{
				name: entry.Name(),
				mode: "40000",
				hash: createTreeObjects(entryPath),
			}
			hashedEntries = append(hashedEntries, he)
		} else {
			he := HashedEntry{
				name: entry.Name(),
				mode: "100644",
				hash: createFileBlobObject(entryPath),
			}
			hashedEntries = append(hashedEntries, he)
		}
	}

	treeObjectContent := ""
	for _, he := range hashedEntries {
		treeObjectContent += fmt.Sprintf("%v %v\x00%v", he.mode, he.name, string(he.hash))
	}

	return createObject("tree", []byte(treeObjectContent))
}

func createFileBlobObject(fp string) []byte {
	fileBytes, err := os.ReadFile(fp)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file %v: %v", fp, err.Error())
		os.Exit(1)
	}
	return createObject("blob", fileBytes)
}

func createObject(objectType string, content []byte) []byte {
	objectContent := fmt.Sprintf("%s %d\x00%s", objectType, len(content), content)
	hash := sha1.Sum([]byte(objectContent))
	compressedContent := compressContent([]byte(objectContent))
	writeObject(hash[:], compressedContent)
	return hash[:]
}

func compressContent(content []byte) []byte {
	var buffer bytes.Buffer
	writer := zlib.NewWriter(&buffer)
	writer.Write(content)
	writer.Close()
	return buffer.Bytes()
}

func writeObject(hash []byte, content []byte) {
	objectsDir := ".git/objects"
	hashString := hex.EncodeToString(hash)
	objectFileDir := filepath.Join(objectsDir, hashString[:2])
	objectFilePath := filepath.Join(objectFileDir, hashString[2:])

	if err := os.MkdirAll(objectFileDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create directory %v: %v", objectFileDir, err.Error())
		os.Exit(1)
	}

	if err := os.WriteFile(objectFilePath, content, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create file %v: %v", objectFilePath, err.Error())
		os.Exit(1)
	}
}

func readAndDecompressFile(filePath string) string {
	fileBytes, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file: %v", filePath)
		os.Exit(1)
	}

	bytesReader := bytes.NewReader(fileBytes)
	zlibReader, err := zlib.NewReader(bytesReader)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating new zlib reader: %v", err)
		os.Exit(1)
	}
	defer zlibReader.Close()

	decompressedBytes, _ := io.ReadAll(zlibReader)
	return string(decompressedBytes)
}

func commitTree() {
	treeSHA := os.Args[2]
	parentCommitSHA := os.Args[4]
	commitMessage := os.Args[6]
	author := "John Doe <john@example.com> 1631234567 -0700"
	committer := "Jane Smith <jane@example.com> 1631234789 -0700"

	content := fmt.Sprintf("tree %v\n", treeSHA)
	content += fmt.Sprintf("parent %v\n", parentCommitSHA)
	content += fmt.Sprintf("author %v\n", author)
	content += fmt.Sprintf("committer %v\n", committer)
	content += "\n" + commitMessage + "\n"

	header := fmt.Sprintf("commit %v\x00", len([]byte(content)))
	payload := header + content

	hash := sha1.Sum([]byte(payload))
	compressedContent := compressContent([]byte(payload))
	writeObject(hash[:], compressedContent)
	fmt.Println(hex.EncodeToString(hash[:]))
}
