// Copyright © 2019 NAME HERE <EMAIL ADDRESS>
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

var (
	overwrite  bool
	noTemplate bool
)
var whiteListDotDirectories = []string{"github", "vscode", "settings", "metadata"}
var whiteListDotFiles = []string{"git", "project", "DS_Store", "classpath", "factorypath", "gitattributes", "gitignore", "cw-settings", "cw-extension"}

// initCmd represents the init command
var initCmd = &cobra.Command{
	Use:   "init [stack]",
	Short: "Initialize an appsody project with a stack and template app",
	Long: `This creates a new appsody project in a local directory or sets up the local dev environment of an existing appsody project. 

With the [stack] argument, this command will setup a new appsody project. It will create an appsody stack config file, unzip a template app, and 
run the stack init script to setup the local dev environment. It is typically run on an empty directory and may fail
if files already exist. See the --overwrite and --no-template options for more details.
Use 'appsody list' to see the available stack options.

Without the [stack] argument, this command must be run on an existing appsody project and will only run the stack init script to 
setup the local dev environment.`,
	Run: func(cmd *cobra.Command, args []string) {
		var index RepoIndex

		var proceedWithTemplate bool

		err := CheckPrereqs()
		if err != nil {
			Warning.logf("Failed to check prerequisites: %v\n", err)
		}

		index.getIndex()
		if len(args) < 1 {
			install()
			os.Exit(1)

		}

		projectType := args[0]

		if len(index.Projects[projectType]) < 1 {
			Error.logf("Could not find a stack with the name %s. Run `appsody list` to see the available stacks or -h for help.", projectType)
			os.Exit(1)
		}
		var projectName = index.Projects[projectType][0].URLs[0]

		Info.log("Running appsody init...")

		// 1. Check for empty directory
		dir, err := os.Getwd()
		if err != nil {
			Error.log("Error getting current directory ", err)
			os.Exit(1)
		}
		appsodyConfigFile := filepath.Join(dir, ".appsody-config.yaml")

		_, err = os.Stat(appsodyConfigFile)
		if err == nil {
			Error.log("Cannot run appsody init <stack> on an existing appsody project.")
			os.Exit(1)
		}

		if noTemplate || overwrite {
			proceedWithTemplate = true
		} else {
			proceedWithTemplate = isFileLaydownSafe(dir)
		}
		// Download and untar

		if !overwrite && !proceedWithTemplate {
			Error.log("Local files exist which may conflict with the template project. If you wish to proceed, try again with the --overwrite option.")
			os.Exit(1)
		}

		Info.logf("Downloading %s template project from %s", projectType, projectName)
		filename := projectType + ".tar.gz"

		err = downloadFile(projectName, filename)
		if err != nil {
			Error.log("Error downloading tar ", err)
			os.Exit(1)
		}
		Info.log("Download complete. Extracting files from ", filename)
		//if noTemplate
		errUntar := untar(filename, noTemplate)

		if dryrun {
			Info.logf("Dry Run - Skipping remove of temporary file for project type: %s project name: %s", projectType, projectName)
		} else {
			err = os.Remove(filename)
			if err != nil {
				Warning.log("Unable to remove temporary file ", filename)
			}
			Info.log("Successfully initialized ", projectType, " project")
		}
		if errUntar != nil {
			Error.log("Error extracting template: ", errUntar)
			// this leave the tar file in the dir
			os.Exit(1)
		}

		install()
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
	initCmd.PersistentFlags().BoolVar(&overwrite, "overwrite", false, "Download and extract the template project, overwriting existing files.")
	initCmd.PersistentFlags().BoolVar(&noTemplate, "no-template", false, "Only create the .appsody-config.yaml file. Do not unzip the template project.")

}

//Runs the .appsody-init.sh/bat files if necessary
func install() {
	Info.log("Setting up the development environment")
	projectDir := getProjectDir()
	platformDefinition := getProjectConfig().Platform

	Debug.logf("Setting up the development environment for projectDir: %s and platform: %s", projectDir, platformDefinition)

	err := extractAndInitialize()
	if err != nil {
		Error.logf("Stack init script failed: %v\nTo try again, run `appsody init` with no arguments.", err)
	}

}

func downloadFile(url string, destFile string) error {
	if dryrun {
		Info.logf("Dry Run -Skipping download of url: %s to destination %s", url, destFile)

	} else {
		outFile, err := os.Create(destFile)
		if err != nil {
			return err
		}
		defer outFile.Close()

		// allow file:// scheme
		t := &http.Transport{}
		t.RegisterProtocol("file", http.NewFileTransport(http.Dir("/")))

		httpClient := &http.Client{Transport: t}
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return err
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return fmt.Errorf("Failed to fetch %s : %s", url, resp.Status)
		}

		_, err = io.Copy(outFile, resp.Body)
		if err != nil {
			return err
		}
		resp.Body.Close()
	}
	return nil
}

func untar(file string, noTemplate bool) error {

	if dryrun {
		Info.log("Dry Run - Skipping untar of file:  ", file)
	} else {
		if !overwrite && !noTemplate {
			err := preCheckTar(file)
			if err != nil {
				return err
			}
		}
		fileReader, err := os.Open(file)
		if err != nil {
			return err
		}

		defer fileReader.Close()
		gzipReader, err := gzip.NewReader(fileReader)
		if err != nil {
			return err
		}
		defer gzipReader.Close()
		tarReader := tar.NewReader(gzipReader)
		for {
			header, err := tarReader.Next()

			if err == io.EOF {
				break
			} else if err != nil {
				return err
			}
			if header == nil {
				continue
			}

			filename := header.Name
			Debug.log("Untar creating ", filename)

			if header.Typeflag == tar.TypeDir && !noTemplate {
				if _, err := os.Stat(filename); err != nil {
					err := os.MkdirAll(filename, 0755)
					if err != nil {
						return err
					}
				}
			} else if header.Typeflag == tar.TypeReg {
				if !noTemplate || (noTemplate && strings.HasSuffix(filename, ".appsody-config.yaml")) {

					f, err := os.OpenFile(filename, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
					if err != nil {
						return err
					}
					_, err = io.Copy(f, tarReader)
					if err != nil {
						return err
					}
					f.Close()
				}
			}

		}
	}
	return nil
}

func isFileLaydownSafe(directory string) bool {

	safe := true
	files, err := ioutil.ReadDir(directory)
	if err != nil {
		Error.logf("Can not read directory %s due to error: %v.", directory, err)
		os.Exit(1)

	}
	for _, f := range files {

		whiteListed := inWhiteList(f.Name())
		if !whiteListed {
			Debug.logf("%s is not in the list of white listed files or directories", f.Name())
			safe = false
		}
	}
	Debug.logf("Returning %v from laydown\n", safe)
	return safe

}

func buildOrList(args []string) string {
	base := ""
	for _, fileName := range args {
		base += fileName
		base += "|"
	}
	if base != "" {
		base = base[:len(base)-1]
	}

	return base
}

func inWhiteList(filename string) bool {
	whiteListTest := "(^(.[/\\\\])?.(" +
		buildOrList(whiteListDotFiles) +
		")$)|(^(.[/\\\\])?.(" + buildOrList(whiteListDotDirectories) + ")[/\\\\]?.*)"

	whiteListRegexp := regexp.MustCompile(whiteListTest)
	isWhiteListed := whiteListRegexp.MatchString(filename)
	Debug.logf("filename %s is in the whitelist %v\n", filename, isWhiteListed)
	return isWhiteListed
}

func preCheckTar(file string) error {
	preCheckOK := true
	fileReader, err := os.Open(file)
	if err != nil {
		return err
	}
	defer fileReader.Close()

	gzipReader, err := gzip.NewReader(fileReader)
	if err != nil {
		return err
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	// precheck the tar for whitelisted files
	for {
		header, err := tarReader.Next()

		if err == io.EOF {

			break
		} else if err != nil {

			return err
		}
		if header == nil {
			continue
		} else {

			if inWhiteList(header.Name) {
				fileInfo, err := os.Stat(header.Name)
				if err == nil {
					if !fileInfo.IsDir() {
						preCheckOK = false
						Warning.log("Conflict: " + header.Name + " exists in the file system and the template project.")

					}

				}
			}
		}
	}
	if !preCheckOK {
		err = errors.New("conflicts exist. If you wish to proceed, try again with the --overwrite option")

	}
	return err
}
func extractAndInitialize() error {

	var err error

	scriptFile := "./.appsody-init.sh"
	if runtime.GOOS == "windows" {
		scriptFile = ".\\.appsody-init.bat"
	}

	scriptFileName := filepath.Base(scriptFile)
	//Determine if we need to run extract
	//We run it only if there is an initialization script to run locally
	//Checking if the script is present on the image
	stackImage := getProjectConfig().Platform
	bashCmd := "find / -type f -name " + scriptFileName
	cmdOptions := []string{"--rm"}
	Debug.log("Attempting to run ", bashCmd, " on image ", stackImage, " with options: ", cmdOptions)
	//DockerRunBashCmd has a pullImage call
	scriptFindOut, err := DockerRunBashCmd(cmdOptions, stackImage, bashCmd)
	if err != nil {
		Error.log("Failed to run the find command for the ", scriptFileName, " on the stack image: ", stackImage)
		os.Exit(1)
	}

	if scriptFindOut == "" {
		Debug.log("There is no initialization script in the image - skipping extract")
		return nil
	}

	workdir := ".appsody_init"

	// run the extract command here
	if !dryrun {
		workdirExists, err := exists(workdir)
		if workdirExists && err == nil {
			err = os.RemoveAll(workdir)
			if err != nil {
				Error.log("Could not remove working dir ", err)
				return err
			}
		}
		// set the --target-dir flag for extract
		targetDir = workdir
		extractCmd.Run(extractCmd, nil)

	} else {
		Info.log("Dry Run skipping extract.")
	}

	scriptPath := filepath.Join(workdir, scriptFile)
	scriptExists, err := exists(scriptPath)

	if scriptExists && err == nil { // if it doesn't exist, don't run it
		Debug.log("Running appsody_init script ", scriptFile)
		execAndWaitWithWorkDir(scriptFile, nil, Info, workdir)
	}

	if !dryrun {
		Debug.log("Removing ", workdir)
		err = os.RemoveAll(workdir)
		if err != nil {
			Error.log("Could not remove working dir ", err)
		}
	}

	return err
}
