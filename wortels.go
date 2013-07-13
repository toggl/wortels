package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	Version                = "0.9"
	SprocketsRequireClause = "//= require "
)

//FIXME: add test cases

var (
	outdir         = flag.String("outdir", filepath.Join("public", "assets"), "folder where to put packaged files")
	verbose        = flag.Bool("verbose", false, "turn on verbose logging")
	version        = flag.Bool("version", false, "print version and exit")
	digest         = flag.String("digest", "", "inject digest into output file names")
	generateDigest = flag.Bool("generatedigest", false, "generate digest from the output source")
	assetPath      = flag.String("assetpath", "", "set assetpath if your assets are not on the path specified in manifest")
	jsCompressor   = flag.String("jscompressor", "closure", "javascript compiler: closure or uglifyjs")
	// Platform specific stuff, will be configured in main
	shellForCommands      = ""
	shasumResultSeparator = ""
)

func main() {
	flag.Parse()

	if *version {
		fmt.Printf("%v\n", Version)
		os.Exit(0)
	}

	// Parse manifest files
	manifestFiles := flag.Args()
	if len(manifestFiles) == 0 {
		fmt.Printf("Specify the manifest file(s) to process\n")
		os.Exit(1)
	}
	if *verbose {
		fmt.Printf("Manifest files: %v (%d)\n", manifestFiles, len(manifestFiles))
	}

	if runtime.GOOS == "windows" {
		shellForCommands = "sh"
		shasumResultSeparator = " *"
	} else {
		shellForCommands = "/bin/sh"
		shasumResultSeparator = "  "
	}

	runtime.GOMAXPROCS(runtime.NumCPU())

	// Create out dir (public/assets)
	if err := os.MkdirAll(*outdir, 0777); err != nil {
		panic(err)
	}

	// Validate js compiler
	if *jsCompressor != "closure" && *jsCompressor != "uglifyjs" {
		fmt.Printf("Invalid Javascript compiler: '%s'", jsCompressor)
		os.Exit(1)
	}

	// Create cache dir
	user, err := user.Current()
	if err != nil {
		panic(err)
	}
	appDir := filepath.Join(user.HomeDir, ".wortels")
	cacheDir := filepath.Join(appDir, "cache", *jsCompressor)
	if err := os.MkdirAll(cacheDir, 0777); err != nil {
		panic(err)
	}

	// Read in file list(s)
	files := make(map[string][]string)
	for _, manifest := range manifestFiles {
		b, err := ioutil.ReadFile(filepath.Join(*assetPath, manifest))
		if err != nil {
			panic(err)
		}
		for _, file := range strings.Split(string(b), "\n") {
			file = strings.TrimSpace(file)
			if file == "" {
				continue
			}
			// Convert Sprockets
			if strings.HasPrefix(file, SprocketsRequireClause) {
				file = strings.Replace(file, SprocketsRequireClause, "", 1)
				if !strings.HasSuffix(file, ".js") {
					file = file + ".js"
				}
			}
			// Ignore comments
			if strings.HasPrefix(file, "//") {
				continue
			}
			// Sprockets manifest files have relative paths
			file = filepath.Join(*assetPath, file)
			// Save the file under asset file list
			files[manifest] = append(files[manifest], file)
		}
	}
	if *verbose {
		fmt.Printf("File list: %v (%d)\n", files, len(files))
	}

	// Populate shasums dictionary
	shasums := make(map[string]string)
	dirs := make(map[string]bool)
	for manifest := range files {
		for _, file := range files[manifest] {
			dirs[filepath.Dir(file)] = true
		}
	}
	for dir, _ := range dirs {
		shasum(filepath.Join(dir, "*"), &shasums)
	}
	if *verbose {
		fmt.Printf("SHA database: %v (%d)\n", shasums, len(shasums))
	}

	// Find out which files need to be recompiled
	uniqueCompilationList := make(map[string]bool)
	for manifest := range files {
		for _, file := range files[manifest] {
			sha, knownFile := shasums[file]
			if !knownFile {
				panic(fmt.Sprintf("File not found in SHA1 database: '%v'\n", file))
			}
			cached := filepath.Join(cacheDir, sha)
			if !fileExists(cached) {
				uniqueCompilationList[file] = true
			}
		}
	}
	var compilationList []string
	for file := range uniqueCompilationList {
		compilationList = append(compilationList, file)
	}
	if *verbose {
		fmt.Printf("Files to compile: %v (%d)\n", compilationList, len(compilationList))
	}

	// Compile
	// http://closure-compiler.googlecode.com/files/compiler-latest.zip
	if len(compilationList) > 0 {
		compilationStart := time.Now()
		portableCompilationList := make([]string, len(compilationList))
		for i, path := range compilationList {
			portableCompilationList[i] = filepath.ToSlash(path)
		}
		cmd := jsCompileCommand(portableCompilationList, appDir)
		if *verbose {
			fmt.Printf("%v\n", cmd)
		}
		b, err := exec.Command(shellForCommands, "-c", cmd).CombinedOutput()
		if err != nil {
			fmt.Printf("%v\n", string(b))
			os.Exit(1)
		}

		// Split compiler output into separate cached files
		var currentFile *os.File
		lines := strings.Split(string(b), "\n")
		lastLine := len(lines) - 1
		for i, line := range lines {
			if strings.HasPrefix(line, "// Input ") {
				if currentFile != nil {
					currentFile.Close()
				}
				i, err := strconv.Atoi(strings.Split(line, "// Input ")[1])
				if err != nil {
					panic(err)
				}
				file := compilationList[i]
				sha := shasums[file]
				cached := filepath.Join(cacheDir, sha)
				if *verbose {
					fmt.Printf("%v (%s)\n", file, sha)
				}
				currentFile, err = os.Create(cached)
				if err != nil {
					panic(err)
				}
				continue
			}
			if currentFile == nil {
				panic(fmt.Sprintf("No file to write to! Line: %s", line))
			}
			currentFile.Write([]byte(line))
			if i != lastLine {
				currentFile.Write([]byte("\n"))
			}
		}
		if currentFile != nil {
			currentFile.Close()
		}
		if *verbose {
			fmt.Printf("JS compiled in %v\n", time.Since(compilationStart))
		}
	}

	// Assemble asset file from compiled files
	var outputFiles []string
	for manifest := range files {
		catList := make([]string, len(files[manifest]))
		for i, file := range files[manifest] {
			catFile := filepath.Join(cacheDir, shasums[file])
			portableCatFile := filepath.ToSlash(catFile)
			catList[i] = portableCatFile
		}
		inputFiles := strings.Join(catList, " ")
		outputFile := filepath.Join(*outdir, filepath.Base(manifest))
		if *digest != "" {
			outputFile = injectDigest(outputFile, *digest)
		}
		portableOutputFile := filepath.ToSlash(outputFile)
		cmd := fmt.Sprintf("cat %s > %s", inputFiles, portableOutputFile)
		if *verbose {
			fmt.Printf("%v\n", cmd)
		}
		_, err = exec.Command(shellForCommands, "-c", cmd).CombinedOutput()
		if err != nil {
			panic(err)
		}
		outputFiles = append(outputFiles, outputFile)
	}
	if *verbose {
		fmt.Printf("Output files: %v\n", outputFiles)
	}

	// Inject digests into output filenames, if no digest is given
	if *generateDigest {
		// Shasum the output files
		outputDigests := make(map[string]string)
		shasum(strings.Join(outputFiles, " "), &outputDigests)

		// Rename files to include the shasum
		for outputFile, sha1 := range outputDigests {
			renamedFile := injectDigest(outputFile, sha1)
			cmd := fmt.Sprintf("mv %s %s", outputFile, renamedFile)
			if *verbose {
				fmt.Printf("%v\n", cmd)
			}
			_, err = exec.Command(shellForCommands, "-c", cmd).CombinedOutput()
			if err != nil {
				panic(err)
			}
		}
	}
}

func jsCompileCommand(portableCompilationList []string, appDir string) string {
	if *jsCompressor == "closure" {
		portableCompilerPath := filepath.ToSlash(filepath.Join(appDir, "compiler-latest", "compiler.jar"))
		return fmt.Sprintf("java -jar %s --warning_level QUIET --compilation_level SIMPLE_OPTIMIZATIONS --formatting print_input_delimiter --js %s",
			portableCompilerPath,
			strings.Join(portableCompilationList, " "))
	} else if *jsCompressor == "uglifyjs" {
		return fmt.Sprintf("uglifyjs %s", strings.Join(portableCompilationList, " "))
	}
	return ""
}

func injectDigest(outputFile, digest string) string {
	currentExt := filepath.Ext(outputFile)
	newExt := "-" + digest + currentExt
	return strings.Replace(outputFile, currentExt, newExt, -1)
}

func shasum(path string, shasums *map[string]string) {
	portablePath := filepath.ToSlash(path)
	cmd := fmt.Sprintf("shasum %s", portablePath)
	if *verbose {
		fmt.Printf("%v\n", cmd)
	}
	b, err := exec.Command(shellForCommands, "-c", cmd).CombinedOutput()
	if err != nil {
		// newer shasum seems to exit status 1 even if there's no error?
		if err.Error() != "exit status 1" {
			panic(err)
		}
	}
	for _, shasumResult := range strings.Split(string(b), "\n") {
		if shasumResult != "" {
			// Ignore shasum messages a la
			// shasum: foo/bar: Is a directory
			if !strings.HasPrefix(shasumResult, "shasum: ") && !strings.HasSuffix(shasumResult, "Is a directory") {
				fields := strings.Split(shasumResult, shasumResultSeparator)

				if len(fields) < 2 {
					panic(fmt.Sprintf("Unexpected shasum result with separator '%s': '%v'\n", shasumResultSeparator, shasumResult))
				}
				sha := fields[0]
				file := filepath.FromSlash(fields[1])
				(*shasums)[file] = sha
			}
		}
	}
}

func fileExists(path string) bool {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return false
		} else {
			panic(err)
		}
	}
	return true
}
