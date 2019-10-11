// Copyright © 2019 IBM Corporation and others.
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
package functest

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/appsody/appsody/cmd/cmdtest"
)

// Simple test for appsody test command. A future enhancement would be to verify the test output
func TestTestSimple(t *testing.T) {

	log.Println("stacksList is: ", stacksList)

	// if stacksList is empty there is nothing to test so return
	if stacksList == "" {
		log.Println("stacksList is empty, exiting test...")
		return
	}

	// replace incubator with appsodyhub to match current naming convention for repos
	stacksList = strings.Replace(stacksList, "incubator", "appsodyhub", -1)

	// split the appsodyStack env variable
	stackRaw := strings.Split(stacksList, " ")

	// loop through the stacks
	for i := range stackRaw {

		log.Println("***Testing stack: ", stackRaw[i], "***")

		// first add the test repo index
		_, cleanup, err := cmdtest.AddLocalFileRepo("LocalTestRepo", "../cmd/testdata/index.yaml")
		if err != nil {
			t.Fatal(err)
		}

		// create a temporary dir to create the project and run the test
		projectDir, err := ioutil.TempDir("", "appsody-test-simple-test")
		if err != nil {
			t.Fatal(err)
		}

		log.Println("Created project dir: " + projectDir)

		// appsody init
		log.Println("Running appsody init...")
		_, err = cmdtest.RunAppsodyCmdExec([]string{"init", stackRaw[i]}, projectDir)
		if err != nil {
			t.Fatal(err)
		}

		// appsody test
		runChannel := make(chan error)
		go func() {
			log.Println("Running appsody test...")
			_, err = cmdtest.RunAppsodyCmdExec([]string{"test"}, projectDir)
			runChannel <- err
		}()

		waitForError := 20 // in seconds
		stillWaiting := true
		log.Println("Waiting to see if test will fail...")
		for stillWaiting {
			select {
			case err = <-runChannel:
				if err != nil {
					// appsody run exited, probably with an error
					t.Fatalf("appsody test quit unexpectedly: %s", err)
				} else {
					fmt.Println("appsody test exited successfully")
					stillWaiting = false
				}
			case <-time.After(time.Duration(waitForError) * time.Second):
				fmt.Printf("appsody test kept running for %d seconds with no error so consider this passed\n", waitForError)
				stillWaiting = false
				// stop the container if it is still up
				_, err = cmdtest.RunAppsodyCmdExec([]string{"stop"}, projectDir)
				if err != nil {
					fmt.Printf("Ignoring error running appsody stop: %s", err)
				}
			}
		}

		// stop and cleanup

		cleanup()
		os.RemoveAll(projectDir)
	}
}
