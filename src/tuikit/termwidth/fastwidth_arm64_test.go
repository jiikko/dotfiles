package termwidth

// archKernel は差分テストが Go 版と突き合わせる相手。production の fastDispWidth は 16 byte 未満を
// Go 版へ回すので、それを通すと短い入力でアセンブリが一度も走らない。ここではアセンブリを直に呼ぶ。
func archKernel(s string) (int, bool) { return fastDispWidthAsm(s) }
