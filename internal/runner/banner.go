package runner

import (
	"fmt"
	"os"
	"runtime/debug"
)

const banner = `
        __        __          ____         
  _____/ /_      / /_  __  __/ / /_  __  __
 / ___/ __/_____/ __ \/ / / / / __ \/ / / /
/ /__/ /_/_____/ / / / /_/ / / / / / /_/ / 
\___/\__/     /_/ /_/\__,_/_/_/ /_/\__,_/  
                                           
`

var version = ""

func getVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

func showBanner() {
	fmt.Fprint(os.Stderr, banner)
	fmt.Fprintf(os.Stderr, "\t%s - CT Log Parser by Arqsz\n\n", getVersion())
}
