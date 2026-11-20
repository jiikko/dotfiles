package check

import (
	"errors"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyops"
	"ja-writing/oracle"
)

// shapeCall は記録の形の検査の呼び出しと、その前に記録を読む json.loads。ok が偽なら、この関数たちの名前でない。
func (e *interp) shapeCall(c oracle.Call) (out outcome, ok bool) {
	e.t.Helper()
	a := c.Args
	switch {
	case c.Module == "json" && c.Function == "loads":
		v, err := pyjson.Loads(a[0].(string))
		if err != nil {
			e.t.Fatalf("記録の形の case の JSON を読めない: %v", err)
		}
		e.save(c, v)
		return outcome{value: v}, true
	case c.Function == "_record_shape":
		wrong, err := RecordShape(e.ref(a[0]).(*pyjson.Object))
		if err != nil {
			return pyException(e, err), true
		}
		return outcome{value: pyjson.Strings(wrong)}, true
	case c.Function == "_fact_gaps_shape":
		var got bool
		err := catch(func() { got = FactGapsShape(e.ref(a[0])) })
		if err != nil {
			return pyException(e, err), true
		}
		return outcome{value: got}, true
	case c.Function == "_words_shape":
		return outcome{value: WordsShape(e.ref(a[0]))}, true
	}
	return outcome{}, false
}

func catch(f func()) (err error) {
	defer pyops.Catch(&err)
	f()
	return nil
}

func pyException(e *interp, err error) outcome {
	e.t.Helper()
	var exc *pyops.Exception
	if !errors.As(err, &exc) {
		e.t.Fatalf("本家の例外でない誤り: %v", err)
	}
	return outcome{exc: &oracle.PyException{Type: exc.Type, Message: exc.Msg}}
}
