package runner

import "fmt"

var version = "dev"

const banner = `
        __        __          ____         
  _____/ /_      / /_  __  __/ / /_  __  __
 / ___/ __/_____/ __ \/ / / / / __ \/ / / /
/ /__/ /_/_____/ / / / /_/ / / / / / /_/ / 
\___/\__/     /_/ /_/\__,_/_/_/ /_/\__,_/  
                                           
`

func showBanner() {
	fmt.Fprint(logWriter(), banner)
	fmt.Fprintf(logWriter(), "\t%s - CT Log Parser by Arqsz\n\n", version)
}
