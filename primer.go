package main

import (
	"fmt"
	"math"
	"sync"
)

func gen(start int, finish int) <-chan int {
	out := make(chan int)
	go func() {
		for i := start; i < finish; i++ {
			out <- i
		}
		close(out)
	}()
	return out
}

type PrimeResult struct {
	Value   int
	IsPrime bool
}

func isprime(n int) bool {
	stop := int(math.Ceil(math.Sqrt(float64(n))))
	for i := 2; i <= stop; i = i + 1 {
		if n%i == 0 && i != n {
			return false
		}
	}
	return true
}

func primer(c <-chan int) <-chan PrimeResult {
	out := make(chan PrimeResult, 50)
	go func() {
		for v := range c {
			test := isprime(v)
			out <- PrimeResult{
				Value:   v,
				IsPrime: test,
			}
		}
		close(out)
	}()
	return out
}

func fprimer(c <-chan PrimeResult) <-chan int {
	out := make(chan int)
	go func() {
		for v := range c {
			if v.IsPrime {
				out <- v.Value
			}
		}
		close(out)
	}()
	return out
}

func merge(cs ...<-chan int) <-chan int {
	out := make(chan int, 5)
	var wg sync.WaitGroup
	wg.Add(len(cs))
	for _, c := range cs {
		go func(c <-chan int) {
			for v := range c {
				out <- v
			}
			wg.Done()
		}(c)
	}
	go func() {
		wg.Wait()
		close(out)
	}()
	return out
}

func main() {
	fmt.Println("hi")
	oneint := gen(1, 5_000_000)
	// oneint := merge(in1)
	isps := merge(
		fprimer(primer(oneint)),
		fprimer(primer(oneint)),
		fprimer(primer(oneint)),
		fprimer(primer(oneint)),
		fprimer(primer(oneint)),
		fprimer(primer(oneint)),
		fprimer(primer(oneint)),
		fprimer(primer(oneint)),
		fprimer(primer(oneint)),
	)
	for range isps {
		// fmt.Printf("value: %+v\n", n)
	}
}
