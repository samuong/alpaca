package main

// Run a group of goroutines and report the first error.
func gogroup[T any](args []T, f func(T) error) error {
	errch := make(chan error)

	gopher := func(arg T) {
		if err := f(arg); err != nil {
			errch <- err
		}
	}

	for _, arg := range args {
		go gopher(arg)
	}

	return <-errch
}
