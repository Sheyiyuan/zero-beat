package sync

import (
	"context"
	"errors"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/server"
)

// Local remotes use the embedded server, including negotiation after a rejected
// push. The stock server in go-git 5.16 rejects unknown "have" objects; Git's
// protocol permits clients to advertise local commits the server has never seen.
type embeddedFileTransport struct{}

func fileServer(ep *transport.Endpoint) (transport.Transport, storer.Storer, error) {
	r, err := git.PlainOpen(ep.Path)
	if err != nil {
		return nil, nil, err
	}
	return server.NewClient(server.MapLoader{ep.String(): r.Storer}), r.Storer, nil
}

func (embeddedFileTransport) NewUploadPackSession(ep *transport.Endpoint, auth transport.AuthMethod) (transport.UploadPackSession, error) {
	t, objects, err := fileServer(ep)
	if err != nil {
		return nil, err
	}
	s, err := t.NewUploadPackSession(ep, auth)
	if err != nil {
		return nil, err
	}
	return &fileUploadSession{UploadPackSession: s, objects: objects}, nil
}

func (embeddedFileTransport) NewReceivePackSession(ep *transport.Endpoint, auth transport.AuthMethod) (transport.ReceivePackSession, error) {
	t, _, err := fileServer(ep)
	if err != nil {
		return nil, err
	}
	return t.NewReceivePackSession(ep, auth)
}

type fileUploadSession struct {
	transport.UploadPackSession
	objects storer.EncodedObjectStorer
}

func (s *fileUploadSession) UploadPack(ctx context.Context, req *packp.UploadPackRequest) (*packp.UploadPackResponse, error) {
	filtered := *req
	filtered.Haves = nil
	for _, h := range req.Haves {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		err := s.objects.HasEncodedObject(h)
		if errors.Is(err, plumbing.ErrObjectNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		filtered.Haves = append(filtered.Haves, h)
	}
	return s.UploadPackSession.UploadPack(ctx, &filtered)
}
