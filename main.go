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
            return root.children[i].originalPath > root.children[j].originalPath
        })
        out = append(out, TOCEntry {
            size: 0,
            name: strings.TrimPrefix(inputDir, root.originalPath),
            timestamp: 0,
            originalPath: root.originalPath,
            isDir: true,
        })
        for _, c := range sortedChildren {
            out = append(out, produceTOC(inputDir, c)...)
        }
        out = append(out, TOCEntry {
            size: 0,
            name: "..",
            timestamp: 0,
            originalPath: root.originalPath,
            isDir: true,
        })
    } else {
        out = append(out, TOCEntry {
            size: root.size,
            name: strings.TrimPrefix(inputDir, root.originalPath),
            timestamp: int32(root.modTime.Unix()),
            originalPath: root.originalPath,
        })
    }
    return out
}

func printVP(toc []TOCEntry, out io.Writer) error {
    out.Write([]byte("VPVP"))
    binary.Write(out, binary.LittleEndian, int32(2))

    var totalSize int32 = 0
    for _, entry := range toc {
        totalSize += entry.size
    }
    binary.Write(out, binary.LittleEndian, totalSize)
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
    var currentOffset int32 = 0
    for _, entry := range toc {
        // offset
        binary.Write(out, binary.LittleEndian, currentOffset)
        // size
        binary.Write(out, binary.LittleEndian, entry.size)
        // path
        out.Write([]byte(path.Base(entry.originalPath)))
        out.Write([]byte("\000"))

        // timestamp
        binary.Write(out, binary.LittleEndian, entry.timestamp)
        if !entry.isDir {
            currentOffset += entry.size
        }
    }
    return nil
}

func main() {
    // TODO: handle 0 args
    inputDir := os.Args[1]
    root, err := walkDir(inputDir)
    if err != nil {
        log.Fatalf("error: %v\n", err)
    }
    toc := produceTOC(inputDir, root)
    err = printVP(toc, os.Stdout)
    if err != nil {
        log.Fatalf("error: %v\n", err)
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