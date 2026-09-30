package vt

// imageStore tracks inline images (sixel, kitty graphics, iTerm2). This is
// a placeholder until the image protocols land.
type imageStore struct{}

func newImageStore() *imageStore { return &imageStore{} }

func (s *imageStore) scrolled(n int)                                {}
func (s *imageStore) evicted(firstAbs int64)                        {}
func (s *imageStore) eraseCells(t *Terminal, l *Line, from, to int) {}
func (s *imageStore) screenSwitched(alt bool)                       {}
func (s *imageStore) reset()                                        {}
func (s *imageStore) resized(t *Terminal)                           {}

type discardDCS struct{}

func (discardDCS) put([]byte)       {}
func (discardDCS) unhook(*Terminal) {}

func (t *Terminal) newSixel(p *params) dcsHandler { return discardDCS{} }

func (t *Terminal) apcDispatch(data []byte)        { t.lastValid = false }
func (t *Terminal) itermImage(val string)          {}
func (t *Terminal) itermMultipartStart(val string) {}
func (t *Terminal) itermMultipartPart(val string)  {}
func (t *Terminal) itermMultipartEnd()             {}
