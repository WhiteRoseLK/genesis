// SPDX-License-Identifier: Apache-2.0

// Command genesis est le binaire du cœur : la seule app à lancer.
package main

import "genesis/internal/cli"

func main() {
	cli.Execute()
}
