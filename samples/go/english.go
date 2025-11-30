package main

type English struct{}

func (English) Greet(name string) string {
	return "Hello, " + name
}
