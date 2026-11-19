package clienttest

import (
	"context"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/client"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/domain"
	"github.com/google/uuid"
)

type htxContactCommand struct {
	ref     uuid.UUID
	contact client.HTXContact
}

// Như các method Fake hiện hữu: scope/vai kiểm bằng integration owner thật;
// Fake giữ tenant, command bền vững trong bộ nhớ và masking thực.
func (f *Fake) ReadHTXContact(ctx context.Context, purpose client.Purpose, ref uuid.UUID, masked bool) (client.HTXContact, error) {
	tid, actor, err := f.start(ctx, "htx_contact_read", purpose, true)
	if err != nil {
		return client.HTXContact{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.htxErased[[2]uuid.UUID{tid, ref}]; ok {
		return client.HTXContact{}, client.ProblemErased
	}
	out, ok := f.htxContacts[[2]uuid.UUID{tid, ref}]
	if !ok {
		return client.HTXContact{}, client.ProblemNotFound
	}
	if masked {
		out.FullName = domain.Label(out.FullName)
		out.Phone = domain.MaskPhone(out.Phone)
		out.Masked = true
	}
	f.log = append(f.log, Access{Op: "htx_contact_read", Purpose: purpose, Ref: ref, Actor: actor})
	return out, nil
}

func (f *Fake) WriteHTXContact(ctx context.Context, purpose client.Purpose, ref, command uuid.UUID, in client.HTXContact) error {
	tid, actor, err := f.start(ctx, "htx_contact_write", purpose, true)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.htxErased[[2]uuid.UUID{tid, ref}]; ok {
		return client.ProblemErased
	}
	if f.htxContacts == nil {
		f.htxContacts = map[[2]uuid.UUID]client.HTXContact{}
		f.htxCommands = map[[2]uuid.UUID]htxContactCommand{}
	}
	phone, err := domain.NormalizePhone(in.Phone)
	if err != nil {
		return problem.Validation.WithField("phone", err.Error())
	}
	in.Phone = phone
	in.FullName = domain.NormalizeName(in.FullName)
	key := [2]uuid.UUID{tid, command}
	if old, ok := f.htxCommands[key]; ok {
		if old.ref != ref || old.contact != in {
			return problem.IdempotencyConflict
		}
		return nil
	}
	f.htxCommands[key] = htxContactCommand{ref: ref, contact: in}
	f.htxContacts[[2]uuid.UUID{tid, ref}] = in
	f.log = append(f.log, Access{Op: "htx_contact_write", Purpose: purpose, Ref: ref, Actor: actor})
	return nil
}

func (f *Fake) ValidateHTXContactErasure(ctx context.Context, purpose client.Purpose, ref uuid.UUID) error {
	_, _, err := f.start(ctx, "htx_contact_erasure_check", purpose, true)
	return err
}

func (f *Fake) EraseHTXContact(ctx context.Context, purpose client.Purpose, ref, command uuid.UUID) error {
	tid, actor, err := f.start(ctx, "htx_contact_erase", purpose, true)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.htxErased == nil {
		f.htxErased = map[[2]uuid.UUID]uuid.UUID{}
	}
	for scope, previous := range f.htxErased {
		if scope[0] == tid && previous == command && scope[1] != ref {
			return problem.IdempotencyConflict
		}
	}
	key := [2]uuid.UUID{tid, ref}
	if _, ok := f.htxErased[key]; ok {
		return nil
	}
	f.htxErased[key] = command
	delete(f.htxContacts, key)
	for key, value := range f.htxCommands {
		if key[0] == tid && value.ref == ref {
			delete(f.htxCommands, key)
		}
	}
	f.log = append(f.log, Access{Op: "htx_contact_erase", Purpose: purpose, Ref: ref, Actor: actor})
	return nil
}
