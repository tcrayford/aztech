package main

import (
    "encoding/binary"
    "fmt"
    "io"
    "io/ioutil"
    "log"
    "os"
    "path"
    "sort"
    "strings"
    "time"
)

type InputFileOrDir struct {
    originalPath string
    size int32
    modTime time.Time
    isDir bool
    children []InputFileOrDir
}

func walkDir(inputDir string) (InputFileOrDir, error) {
    fileInfos, err := ioutil.ReadDir(inputDir)
    if err != nil {
        return InputFileOrDir{"err", 0, time.Unix(0,0), false, []InputFileOrDir{}}, err
    }
    children := make([]InputFileOrDir, 0)
    for _, f := range fileInfos {
        if f.IsDir() {
            child, err := walkDir(path.Join(inputDir, f.Name()))
            if err != nil {
                return InputFileOrDir{"err", 0, time.Unix(0,0), false, []InputFileOrDir{}}, err
            }

            children = append(children, child)
        } else {
            children = append(children, convertFileInfo(inputDir, f))
        }
    }
    return InputFileOrDir {
        originalPath: inputDir,
        size: 0,
        modTime: time.Unix(0, 0),
        isDir: true,
        children: children,
    }, nil
}

func convertFileInfo(root string, f os.FileInfo) InputFileOrDir {
    return InputFileOrDir{
        originalPath: path.Join(root, f.Name()),
        size: int32(f.Size()),
        modTime: f.ModTime(),
        isDir: false,
        children: []InputFileOrDir{},
    }
}

func printInputFileOrDir(f InputFileOrDir, level int) {
    indent := strings.Repeat(" ", level * 2)
    if f.isDir {
        fmt.Printf("%sdir:  %v\n", indent, f.originalPath)
        for _, c := range f.children {
            printInputFileOrDir(c, level + 1)
        }
    } else {
        fmt.Printf("%sfile: %v\n", indent, f.originalPath)
    }
}

func produceTOC(inputDir string, root InputFileOrDir) []TOCEntry {
    out := []TOCEntry{}
    if root.isDir {
        sortedChildren := root.children[:]
        sort.Slice(sortedChildren, func(i, j int) bool {
            return path.Base(root.children[i].originalPath) < path.Base(root.children[j].originalPath)
        })
        out = append(out, TOCEntry {
            size: 0,
            name: path.Base(root.originalPath),
            timestamp: 0,
            originalPath: root.originalPath,
            isDir: true,
        })
        for _, c := range sortedChildren {
            recursed := produceTOC(inputDir, c)
            out = append(out, recursed...)
        }
        out = append(out, TOCEntry {
            size: 0,
            name: "..",
            timestamp: 0,
            originalPath: path.Join(root.originalPath, ".."),
            isDir: true,
        })
        if strings.Contains(root.originalPath, "data/hud") {
            fmt.Fprintf(os.Stderr, "out last=%v\n", out[len(out) - 1])
        }
    } else {
        out = append(out, TOCEntry {
            size: root.size,
            name: path.Base(root.originalPath),
            timestamp: int32(root.modTime.Unix()),
            originalPath: root.originalPath,
        })
    }
    return out
}

func printVP(in InputFileOrDir, toc []TOCEntry, out io.Writer) error {
    out.Write([]byte("VPVP"))
    binary.Write(out, binary.LittleEndian, int32(2))

    var totalSize int32 = 0
    for _, entry := range toc {
        totalSize += entry.size
        if totalSize < 0 {
            return fmt.Errorf("overflowed totalSize, %v producing %v", totalSize, in.originalPath)
        }
    }
    binary.Write(out, binary.LittleEndian, totalSize + 16)
    binary.Write(out, binary.LittleEndian, int32(len(toc)))
    for _, entry := range toc {
        if entry.isDir {
        } else {
            f, err := os.Open(entry.originalPath)
            if err != nil {
                return err
            }

            _, err = io.Copy(out, f)
            if err != nil {
                return err
            }
        }
    }
    var currentOffset int32 = 16
    for _, entry := range toc {
        fmt.Fprintf(os.Stderr, "processing header for '%q', offset=%d size=%d\n", entry.name, currentOffset, entry.size)
        // offset
        binary.Write(out, binary.LittleEndian, currentOffset)
        // size
        binary.Write(out, binary.LittleEndian, entry.size)
        // path
        remainingBytes := 32 - (len(entry.name) + 1)
        out.Write([]byte(entry.name))
        out.Write([]byte("\000"))
        out.Write([]byte(strings.Repeat("\000", remainingBytes)))

        // timestamp
        binary.Write(out, binary.LittleEndian, entry.timestamp)
        if !entry.isDir {
            currentOffset += entry.size
        }
        if totalSize < 0 {
            return fmt.Errorf("overflowed totalSize, %v producing %v", totalSize, in.originalPath)
        }
    }
    return nil
}

// function splitTOCs splits
// TOC entries to ensure nothing overflows max size
func splitTOCs(toc []TOCEntry) ([][]TOCEntry) {
    out := [][]TOCEntry{}
    var totalSize int32 = 0
    current := []TOCEntry{}
    currentDirs := []TOCEntry{}
    for _, entry := range toc {
        if entry.isDir {
            currentDirs = append(currentDirs, entry)
            if strings.Contains(entry.originalPath, "data/hud") {
                fmt.Fprintf(os.Stderr, "current dirs appending='%q' name='%q'\n", entry.originalPath, entry.name)
            }
        }
        totalSize += entry.size
        if totalSize < 0 || totalSize > 1000000000 {
            out = append(out, current)
            totalSize = 0
            current = []TOCEntry{}
            current = append(current, currentDirs...)
        } else {
            current = append(current, entry)
        }
    }
    out = append(out, current)
    return out
}

func main() {
    // TODO: handle 0 args
    inputDir := os.Args[1]

    dataDir, err := os.Stat(path.Join(inputDir, "data"))
    if err != nil {
        log.Fatalf("error: %v\n", err)
    }
    if !dataDir.Mode().IsDir() {
        log.Fatalf("error: %v is not a directory\n", path.Join(inputDir, "data"))
    }

    root, err := walkDir(inputDir)

    if err != nil {
        log.Fatalf("error: %v\n", err)
    }
    // we break up one toc per folder in data, for now
    for _, child := range root.children {
        if path.Base(child.originalPath) == "data" {
            for _, dataChild := range child.children {
                newChild := InputFileOrDir {
                    originalPath: "data",
                    size: 0,
                    modTime: time.Unix(0, 0),
                    isDir: true,
                    children: []InputFileOrDir{ dataChild },
                }
                toc := produceTOC(inputDir, newChild)
                split := splitTOCs(toc)
                // fmt.Fprintf(os.Stderr, "processing data child %s with %d children, found %d vps\n", path.Base(dataChild.originalPath), len(dataChild.children), len(split))
                for subtocNumber, subtoc := range split {
                    var filename string
                    if len(split) == 1 {
                        filename = fmt.Sprintf("%s.vp", path.Base(dataChild.originalPath))
                    } else {
                        filename = fmt.Sprintf("%s-%02d.vp", path.Base(dataChild.originalPath), subtocNumber + 1)
                    }
                    filepath := path.Join("tmp", filename)
                    if _, err := os.Stat(filepath); os.IsNotExist(err) {
                        f, err := os.Create(filepath)
                        if err != nil {
                            log.Fatalf("error: %v\n", err)
                        }
                        err = printVP(dataChild, subtoc, f)
                        if err != nil {
                            log.Fatalf("error: %v\n", err)
                        }
                    } else {
                        log.Fatalf("error: %v already exists\n", filepath)
                    }
                }
            }
        }
    }
}

//TOC:
//ALL are little-endian
//=====
//int (4 bytes) - position
//int (4 bytes) - size
//char (32 bytes) - name
//timestamp (4 bytes) - time since 1970 in seconds
//=====

type TOCEntry struct {
    size int32
    name string
    timestamp int32

    // the original path of the file
    originalPath string

    isDir bool
}
