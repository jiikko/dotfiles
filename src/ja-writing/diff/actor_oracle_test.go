package diff

import (
	"testing"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyre"
	"ja-writing/internal/pyunicode"
	"ja-writing/oracle"
	"ja-writing/oracle/replay"
)

// 群 AB の段と下の関数の記録の突き合わせ。

var actorHits = map[string]int{}

// actorSources は覚えた _ActorSource (名前 → 原文の並び)。
var actorSources = map[string][]string{}

func actorConstantValue(t *testing.T, name string) pyjson.Value {
	t.Helper()
	strs := func(xs []string) pyjson.Value { return replay.Strings(xs) }
	switch name {
	case "_ACTOR_SPECIFIC":
		return strs(actorSpecific)
	case "_ACTOR_GENERIC":
		return strs(actorGeneric)
	case "_ACTOR_PERSON":
		return strs(actorPerson)
	case "_ACTOR_READER":
		return sortedSet(actorReaderSet)
	case "_ACTOR_CHAR":
		return actorChar
	case "_ACTOR_PREFIX_CHAR":
		return lazyValue(actorPrefixChar)
	case "_ACTOR_NAME_CHAR":
		return lazyValue(actorNameChar)
	case "_ACTOR_PREFIX_MAX":
		return int64(actorPrefixMax)
	case "_ACTOR_HEAD":
		return lazyValue(actorHeadRe)
	case "_ACTOR_HONORIFIC":
		return lazyValue(actorHonorific)
	case "_ACTOR_PERSON_RE":
		return lazyValue(actorPersonRe)
	case "_ACTOR_TITLE":
		return lazyValue(actorTitle)
	case "_ACTOR_TITLE_BAD":
		return sortedSet(actorTitleBadSet)
	case "_ACTOR_NOT_TITLE":
		return sortedSet(actorNotTitleSet)
	case "_ACTOR_ORG":
		return lazyValue(actorOrg)
	case "_ACTOR_ORG_END":
		return lazyValue(actorOrgEnd)
	case "_ACTOR_ORG_STEM_BAD":
		return sortedSet(actorOrgStemBadSet)
	case "_ACTOR_ORG_NUMBERED":
		return lazyValue(actorOrgNum)
	case "_ACTOR_ORG_PART":
		return lazyValue(actorOrgPart)
	case "_ACTOR_NO_HITO":
		return lazyValue(actorNoHito)
	case "_ACTOR_OF_HEADS":
		return sortedSet(actorOfHeadsSet)
	case "_ACTOR_SWAP":
		return strs(actorSwap)
	case "_ACTOR_SAME":
		return replay.MapList(actorSame, func(g []string) pyjson.Value { return strs(g) })
	case "_ACTOR_VERB_NOUN":
		o := pyjson.NewObject()
		o.Set("サポート", lazyValue(actorVerbSupport))
		return replay.SortKeys(o)
	case "_ACTOR_SELF_REF":
		return lazyValue(actorSelfRef)
	case "_ACTOR_SUBJECT":
		return lazyValue(actorSubject)
	case "_ACTOR_RECIPIENT":
		return lazyValue(actorRecipient)
	case "_ACTOR_NOTIFY":
		return lazyValue(actorNotify)
	case "_ACTOR_NOTIFY_WINDOW":
		return int64(actorNotifyWindow)
	case "_ACTOR_FROM":
		return lazyValue(actorFrom)
	case "_ACTOR_CORE":
		return lazyValue(actorCoreRe)
	case "_ACTOR_SKIP_KINDS":
		return sortedSet(actorSkipSet)
	}
	t.Fatalf("定数 %s を Go に持っていない", name)
	return nil
}

func mentionsValue(ms []*actorMention) pyjson.Value {
	return replay.MapList(ms, func(m *actorMention) pyjson.Value { return m.value() })
}

// invokeActor は群 AB の呼び出し 1 つの結果。build は、直前の _build_full_diff の呼び出しの引数。
func invokeActor(t *testing.T, c oracle.Call, build []any) pyjson.Value {
	t.Helper()
	a := c.Args
	switch c.Function {
	case "_actor_norm":
		return actorNorm(argString(t, a[0]))
	case "_actor_core":
		return actorCore(argString(t, a[0]))
	case "_actor_prefix_start":
		text := argString(t, a[0])
		return int64(actorPrefixStart(pyre.NewText(text), argInt(t, a[1]), argInt(t, a[2]), argInt(t, a[3])))
	case "_actor_mentions":
		ms := actorMentions(argString(t, a[0]))
		for _, m := range ms {
			actorHits["mention:"+m.kind]++
			if m.modifier != nil {
				actorHits["mention:modifier"]++
			}
			if m.prefix != "" {
				actorHits["mention:prefix"]++
			}
		}
		if len(ms) == 0 {
			actorHits["mention:(none)"]++
		}
		return mentionsValue(ms)
	case "_actor_frame":
		frame := actorFrameOf(pyre.NewText(argString(t, a[0])), argInt(t, a[1]))
		actorHits["frame:"+frame]++
		if frame == "" {
			return nil
		}
		return frame
	case "_actor_candidates":
		text := argString(t, a[0])
		name := refName(t, a[1])
		texts, ok := actorSources[name]
		if !ok {
			t.Fatalf("覚えていない _ActorSource %s", name)
		}
		for _, m := range actorMentions(pyunicode.Normalize(pyunicode.NFKC, text)) {
			actorHits["mention:"+m.kind]++
			if m.modifier != nil {
				actorHits["mention:modifier"]++
			}
			if m.prefix != "" {
				actorHits["mention:prefix"]++
			}
		}
		got := actorCandidates(text, newActorSource(texts))
		for _, g := range got {
			actorHits["candidate:tier"+g.tier+":"+g.frame]++
			if g.narrowed != "" {
				actorHits["candidate:narrowed"]++
			}
		}
		if len(got) == 0 {
			actorHits["candidate:(none)"]++
		}
		return replay.SortKeys(replay.MapList(got, actorCandidate.value))
	}
	t.Fatalf("関数 %s を Go に持っていない", c.Function)
	return nil
}

// invokeAddedActors は段 _added_actors (groups は直前の _build_full_diff と _sentence_pairs と同じ組から作り直す)。
func invokeAddedActors(t *testing.T, c oracle.Call, build []any) pyjson.Value {
	t.Helper()
	ctx := reviewContextOf(t, build)
	var materials []string
	for _, m := range c.Args[2].([]any) {
		materials = append(materials, argString(t, m))
	}
	got := ctx.s.addedActors(ctx.groups, materials)
	items := got.([]pyjson.Value)
	for _, item := range items {
		list, _ := item.(*pyjson.Object).Get("actors")
		for _, actor := range list.([]pyjson.Value) {
			tier, _ := actor.(*pyjson.Object).Get("tier")
			actorHits["added:tier"+tier.(string)]++
		}
		if old, _ := item.(*pyjson.Object).Get("old"); old == nil {
			actorHits["added:no_old"]++
		}
	}
	if len(items) == 0 {
		actorHits["added:(none)"]++
	}
	if len(materials) > 0 {
		actorHits["added:with_material"]++
	}
	return replay.SortKeys(got)
}

// checkActorHits は群 AB の記録が、候補が出る組と何も出ない組のどちらにも十分届いていることを確かめる。
func checkActorHits(t *testing.T) {
	t.Helper()
	for _, want := range []string{
		"mention:role", "mention:person", "mention:title", "mention:org", "mention:nohito", "mention:modifier", "mention:prefix", "mention:(none)",
		"frame:subject", "frame:recipient", "frame:",
		"candidate:tierA:recipient", "candidate:tierB:subject", "candidate:tierC:subject", "candidate:tierW:subject", "candidate:narrowed", "candidate:(none)",
		"added:tierA", "added:tierB", "added:tierC", "added:tierW", "added:no_old", "added:(none)", "added:with_material",
	} {
		if actorHits[want] < 20 {
			t.Errorf("群 AB の記録が %s に届いた回数は %d (20 回以上が要る)", want, actorHits[want])
		}
	}
	t.Logf("群 AB の記録の届き方: %v", actorHits)
}
