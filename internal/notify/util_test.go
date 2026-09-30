package notify

import "os"

func readFile(p string) (string, error) {
	b, err := os.ReadFile(p)
	return string(b), err
}
