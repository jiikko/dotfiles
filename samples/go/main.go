package main

import "fmt"

func main() {
	var g Greeter

	g = English{}
	fmt.Println(g.Greet("Coc"))

	g = Japanese{}
	fmt.Println(g.Greet("Coc"))
}
