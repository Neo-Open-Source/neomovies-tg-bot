package neomovies

import "os"

func init() {
	getenv = os.Getenv
}
