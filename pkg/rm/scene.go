package rm

import (
	"container/heap"
	"sort"
)

type Sequence struct {
	items map[CrdtID]SequenceItem
}

func newSequence() *Sequence { return &Sequence{items: map[CrdtID]SequenceItem{}} }

func (s *Sequence) add(item SequenceItem) { s.items[item.ItemID] = item }

func (s *Sequence) Len() int { return len(s.items) }

func (s *Sequence) Values() []any {
	ids := s.Order()
	out := make([]any, 0, len(ids))
	for _, id := range ids {
		out = append(out, s.items[id].Value)
	}
	return out
}

type orderNode struct {
	id    CrdtID
	kind  int
	valid bool
}

type nodeHeap []orderNode

func (h nodeHeap) Len() int { return len(h) }
func (h nodeHeap) Less(i, j int) bool {
	if h[i].kind != h[j].kind {
		return h[i].kind < h[j].kind
	}
	if h[i].id.Part1 != h[j].id.Part1 {
		return h[i].id.Part1 > h[j].id.Part1
	}
	return h[i].id.Part2 < h[j].id.Part2
}
func (h nodeHeap) Swap(i, j int)  { h[i], h[j] = h[j], h[i] }
func (h *nodeHeap) Push(x any)    { *h = append(*h, x.(orderNode)) }
func (h *nodeHeap) Pop() any      { old := *h; n := len(old); x := old[n-1]; *h = old[:n-1]; return x }
func startNode() orderNode        { return orderNode{kind: 0} }
func endNode() orderNode          { return orderNode{kind: 2} }
func itemNode(id CrdtID) orderNode { return orderNode{id: id, kind: 1, valid: true} }

func (s *Sequence) Order() []CrdtID {
	if len(s.items) == 0 {
		return nil
	}
	side := func(id CrdtID, left bool) orderNode {
		if _, ok := s.items[id]; ok {
			return itemNode(id)
		}
		if left {
			return startNode()
		}
		return endNode()
	}
	inDegree := map[orderNode]int{startNode(): 0, endNode(): 0}
	dependents := map[orderNode][]orderNode{}
	for _, item := range s.items {
		node := itemNode(item.ItemID)
		left := side(item.LeftID, true)
		right := side(item.RightID, false)
		inDegree[node]++
		dependents[left] = append(dependents[left], node)
		inDegree[right]++
		dependents[node] = append(dependents[node], right)
		if _, ok := inDegree[left]; !ok {
			inDegree[left] = 0
		}
	}
	ready := &nodeHeap{}
	for node, degree := range inDegree {
		if degree == 0 {
			heap.Push(ready, node)
		}
	}
	out := make([]CrdtID, 0, len(s.items))
	for ready.Len() > 0 {
		node := heap.Pop(ready).(orderNode)
		if node.valid {
			out = append(out, node.id)
		}
		if node.kind == 2 {
			break
		}
		for _, dep := range dependents[node] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				heap.Push(ready, dep)
			}
		}
	}
	if len(out) < len(s.items) {
		seen := map[CrdtID]bool{}
		for _, id := range out {
			seen[id] = true
		}
		rest := make([]CrdtID, 0, len(s.items)-len(out))
		for id := range s.items {
			if !seen[id] {
				rest = append(rest, id)
			}
		}
		sort.Slice(rest, func(i, j int) bool {
			if rest[i].Part1 != rest[j].Part1 {
				return rest[i].Part1 < rest[j].Part1
			}
			return rest[i].Part2 < rest[j].Part2
		})
		out = append(out, rest...)
	}
	return out
}

func (s *Scene) Layers() []Layer {
	var layers []Layer
	for _, child := range s.Root.Children.Values() {
		group, ok := child.(*Group)
		if !ok {
			continue
		}
		layer := Layer{Name: group.Label.Value, Visible: group.Visible.Value}
		collect(group, &layer)
		layers = append(layers, layer)
	}
	if len(layers) == 0 {
		layer := Layer{Name: "Layer 1", Visible: true}
		collect(s.Root, &layer)
		if len(layer.Lines) > 0 || len(layer.Highlights) > 0 {
			layers = append(layers, layer)
		}
	}
	return layers
}

func collect(group *Group, layer *Layer) {
	for _, child := range group.Children.Values() {
		switch v := child.(type) {
		case *Group:
			collect(v, layer)
		case *Line:
			layer.Lines = append(layer.Lines, v)
		case *GlyphRange:
			layer.Highlights = append(layer.Highlights, v)
		}
	}
}

func (s *Scene) Lines() []*Line {
	var out []*Line
	for _, layer := range s.Layers() {
		out = append(out, layer.Lines...)
	}
	return out
}

func (s *Scene) Highlights() []*GlyphRange {
	var out []*GlyphRange
	for _, layer := range s.Layers() {
		out = append(out, layer.Highlights...)
	}
	return out
}

func (s *Scene) IsEmpty() bool {
	for _, layer := range s.Layers() {
		if len(layer.Lines) > 0 || len(layer.Highlights) > 0 {
			return false
		}
	}
	return true
}
