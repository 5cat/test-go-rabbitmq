package main

import "testing"

func TestMain(t *testing.T) {
	oneint := gen(1, 10_000_000)
    n_primers := 50
    primers := make([]<-chan int, n_primers)
    for i := 0; i < n_primers; i++ {
        primers[i] = fprimer(primer(oneint))
    }
	isps := merge(primers...)
    for range isps {}
}
