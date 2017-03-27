package zap

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/sourcegraph/jsonrpc2"
	"github.com/sourcegraph/zap/internal/debugutil"
	"github.com/sourcegraph/zap/ot"
	"github.com/sourcegraph/zap/server/refdb"
)

func (s *Server) handleRefUpdateFromUpstream(ctx context.Context, logger log.Logger, params RefUpdateDownstreamParams, endpoint string) error {
	CheckRefName(params.RefIdentifier.Ref)

	if s.TestBlockHandleRefUpdateFromUpstream != nil {
		s.TestBlockHandleRefUpdateFromUpstream <- params
	}

	if err := params.validate(); err != nil {
		return &jsonrpc2.Error{
			Code:    jsonrpc2.CodeInvalidParams,
			Message: "invalid params for ref update from upstream: " + err.Error(),
		}
	}

	// Find the local repo.
	repo, localRepoName, remote, err := s.findLocalRepo(params.RefIdentifier.Repo, endpoint)
	if err != nil {
		return err
	}
	if repo == nil {
		return &jsonrpc2.Error{
			Code:    int64(ErrorCodeRepoNotExists),
			Message: fmt.Sprintf("ref update from upstream failed because no local repo is tracking remote repo %q at endpoint %q", params.RefIdentifier.Repo, endpoint),
		}
	}
	params.RefIdentifier.Repo = localRepoName

	// Update the remote tracking branch.
	remoteTrackingParams := params
	remoteTrackingParams.Ref = remoteTrackingBranchRef(remote, params.RefIdentifier.Ref)
	remoteTrackingParams.Ack = false
	if err := s.updateRemoteTrackingRef(ctx, logger, repo, remoteTrackingParams); err != nil {
		return err
	}

	// Update the local tracking branch for this upstream branch, if any.
	repoConfig, err := repo.getConfig()
	if err != nil {
		return err
	}
	ref := repo.refdb.Lookup(params.RefIdentifier.Ref)
	defer ref.Unlock()
	if refConfig, ok := repoConfig.Refs[params.RefIdentifier.Ref]; ok && refConfig.Upstream == remote {
		if ref.Ref == nil {
			level.Warn(logger).Log("upstream-configured-for-nonexistent-ref", params.RefIdentifier.Ref)
		} else {
			if err := s.updateLocalTrackingRefAfterUpstreamUpdate(ctx, logger, repo, ref, params, refConfig); err != nil {
				return err
			}
		}
	} else {
		level.Debug(logger).Log("no-local-ref-downstream-of", params.RefIdentifier.Ref)
	}
	return nil
}

func (s *Server) updateRemoteTrackingRef(ctx context.Context, logger log.Logger, repo *serverRepo, params RefUpdateDownstreamParams) error {
	CheckRefName(params.RefIdentifier.Ref)

	logger = log.With(logger, "update-remote-tracking-ref", params.RefIdentifier.Ref, "params", params)
	level.Debug(logger).Log()

	timer := time.AfterFunc(5*time.Second, func() {
		level.Warn(logger).Log("delay", "taking a long time, possible deadlock")
	})
	defer timer.Stop()

	debugutil.SimulateLatency()

	ref := repo.refdb.Lookup(params.RefIdentifier.Ref)
	defer ref.Unlock()
	if params.Ack {
		// Nothing to do.
	} else if params.Delete {
		// Delete ref.
		if ref.Ref != nil {
			if err := repo.refdb.Delete(ref); err != nil {
				return err
			}
		} else {
			level.Warn(logger).Log("delete-of-nonexistent-ref", "")
		}
	} else {
		if params.State != nil {
			ref.Ref = &refdb.Ref{
				Name: params.RefIdentifier.Ref,
				Object: serverRef{
					gitBase:   params.State.GitBase,
					gitBranch: params.State.GitBranch,
					ot:        &ot.Proxy{},
				},
			}

			for _, op := range params.State.History {
				// OK to discard the RecvFromUpstream transformed op
				// return value because we know otHandler's history
				// started out empty (because we just created it).
				if _, err := ref.Ref.Object.(serverRef).ot.RecvFromUpstream(logger, op); err != nil {
					return err
				}
			}
		} else if params.Op != nil {
			if ref.Ref == nil {
				return &jsonrpc2.Error{
					Code:    int64(ErrorCodeRefNotExists),
					Message: fmt.Sprintf("received upstream op for remote tracking branch %q but the branch does not exist", params.RefIdentifier.Ref),
				}
			}
			if err := compareRefBaseInfo(*params.Current, ref.Ref.Object.(serverRef)); err != nil {
				return &jsonrpc2.Error{
					Code:    int64(ErrorCodeRefConflict),
					Message: fmt.Sprintf("received upstream op for remote tracking branch %q with conflicting ref state: %s", params.RefIdentifier.Ref, err),
				}
			}
			xop, err := ref.Ref.Object.(serverRef).ot.RecvFromUpstream(logger, *params.Op)
			if err != nil {
				return err
			}
			if op, xop := ot.NormalizeWorkspaceOp(*params.Op), ot.NormalizeWorkspaceOp(xop); !reflect.DeepEqual(op, xop) {
				// We don't expect this to happen since this branch should only
				// get ops from server and transforms shouldn't be necessary.
				// This used to be a panic, but this condition was happening
				// during rapid inserts and undo operations (suspect latency involved).
				// Everything seems to work with the panic removed so now we just log
				// a warning when it happens and avoid crashing the server.
				level.Warn(logger).Log("unexpected-transform-ops-for-ref", ref.Ref.Name, "op", op, "xop", xop, "history", fmt.Sprintf("%v", ref.Ref.Object.(serverRef).history()), "buf", ref.Ref.Object.(serverRef).ot.Buf, "wait", ref.Ref.Object.(serverRef).ot.Wait)
			}
		}

		if err := repo.refdb.Write(ref); err != nil {
			return err
		}
	}

	return s.broadcastRefUpdate(ctx, logger, nil, &params, nil)
}

func (s *Server) updateLocalTrackingRefAfterUpstreamUpdate(ctx context.Context, logger log.Logger, repo *serverRepo, ref refdb.OwnedRef, params RefUpdateDownstreamParams, refConfig RefConfiguration) error {
	CheckRefName(params.RefIdentifier.Ref)

	logger = log.With(logger, "update-local-tracking-ref", params.RefIdentifier.Ref)
	level.Info(logger).Log("params", params)

	timer := time.AfterFunc(5*time.Second, func() {
		level.Warn(logger).Log("delay", "taking a long time, possible deadlock")
	})
	defer timer.Stop()

	// If this ref is configured to overwrite its upstream, then
	// refuse anything from the upstream except ops.
	//
	// TODO(sqs): in the future, provide a way like `git pull -f` for
	// users to explicitly accept overwrites from upstream.
	if refConfig.Overwrite && (params.Delete || params.State != nil) {
		level.Debug(logger).Log("refusing-non-op-update", "")
		return nil
	}

	debugutil.SimulateLatency()

	if params.Delete {
		if err := repo.refdb.Delete(ref); err != nil {
			return err
		}
	} else {
		if params.Current != nil {
			if err := compareRefBaseInfo(*params.Current, ref.Ref.Object.(serverRef)); err != nil {
				return &jsonrpc2.Error{
					Code:    int64(ErrorCodeRefConflict),
					Message: fmt.Sprintf("received upstream op for local tracking branch %q with conflicting ref state: %s", params.RefIdentifier.Ref, err),
				}
			}
		}

		switch {
		case params.Ack:
			// State updates get acked, too, but those do not involve OT.
			if params.Op != nil {
				if err := ref.Ref.Object.(serverRef).ot.AckFromUpstream(logger); err != nil {
					if err == ot.ErrNoPendingOperation {
						level.Error(logger).Log("received-ack-for-previous-generation-of-ref", "")
						// NOTE: ErrNoPendingOperation occurs when
						// this server's ref was recently updated but
						// its RefBaseInfo remains the same, and it
						// receives a slightly delayed upstream
						// update. It currently has no way to know
						// that the ack was for the previous ref.
						//
						// TODO(sqs): add a way to know we can
						// definitely ignore these, and make it so the
						// same problem could never occur when
						// receiving actual ops.
						return nil
					}
					return err
				}
			}

		case params.State != nil:
			// If this is the HEAD ref of a workspace, we need to go
			// via the workspace to reset the state, since we need to
			// change actual files on disk.
			isWorkspaceHEAD := false
			if headRef := repo.refdb.LookupShared("HEAD"); headRef != nil && headRef.Target == ref.Ref.Name {
				isWorkspaceHEAD = true
				level.Info(logger).Log("workspace-checkout", "")
				repo.mu.Lock()
				ws := repo.workspace
				repo.mu.Unlock()
				if ws == nil {
					panic(fmt.Sprintf("during local tracking ref update of %q, HEAD points to it but it has no workspace", ref.Ref.Name))
				}
				if _, err := ws.Checkout(ctx, logger, false, ref.Ref.Name, params.State.GitBase, params.State.GitBranch, params.State.History, nil); err != nil {
					return fmt.Errorf("during local tracking ref update, workspace checkout failed: %s", err)
				}
			}

			oldRefObj := ref.Ref.Object.(serverRef)
			otHandler := &ot.Proxy{
				SendToUpstream: oldRefObj.ot.SendToUpstream,
			}
			if !isWorkspaceHEAD {
				// Don't call Apply in our loop over
				// params.State.History, or else we'll double-apply
				// ops we just applied in the workspace Checkout call
				// above.
				otHandler.Apply = oldRefObj.ot.Apply
			}
			for _, op := range params.State.History {
				// OK to discard the RecvFromUpstream transformed op
				// return value because we know otHandler's history
				// started out empty (because we just created it).
				if _, err := otHandler.RecvFromUpstream(logger, op); err != nil {
					return err
				}
			}
			if isWorkspaceHEAD {
				repo.mu.Lock()
				ws := repo.workspace
				repo.mu.Unlock()
				otHandler.Apply = func(logger log.Logger, op ot.WorkspaceOp) error {
					return ws.Apply(ctx, logger, op)
				}
			}
			ref.Ref.Object = serverRef{
				gitBase:   params.State.GitBase,
				gitBranch: params.State.GitBranch,
				ot:        otHandler,
			}

		case params.Op != nil:
			xop, err := ref.Ref.Object.(serverRef).ot.RecvFromUpstream(logger, *params.Op)
			if err != nil {
				return err
			}
			params.Op = &xop
			debugutil.SimulateLatency()
		}
		if err := repo.refdb.Write(ref); err != nil {
			return err
		}
	}

	// Don't broadcast acks to clients, since we already immediately
	// ack clients.
	if !params.Ack {
		if err := s.broadcastRefUpdate(ctx, logger, nil, &params, nil); err != nil {
			return err
		}
	}
	return nil
}

func compareRefPointerInfo(p RefPointer, r serverRef) error {
	var diffs []string
	if p.GitBase != r.gitBase {
		diffs = append(diffs, fmt.Sprintf("git base: %q != %q", p.GitBase, r.gitBase))
	}
	if p.GitBranch != r.gitBranch {
		diffs = append(diffs, fmt.Sprintf("git branch: %q != %q", p.GitBranch, r.gitBranch))
	}
	if p.Rev != r.ot.Rev() {
		diffs = append(diffs, fmt.Sprintf("rev: %d != %d", p.Rev, r.ot.Rev()))
	}
	if len(diffs) == 0 {
		return nil
	}
	return errors.New(strings.Join(diffs, ", "))
}

func compareRefBaseInfo(p RefBaseInfo, r serverRef) error {
	var diffs []string
	if p.GitBase != r.gitBase {
		diffs = append(diffs, fmt.Sprintf("git base: %q != %q", r.gitBase, p.GitBase))
	}
	if p.GitBranch != r.gitBranch {
		diffs = append(diffs, fmt.Sprintf("git branch: %q != %q", r.gitBranch, p.GitBranch))
	}
	if len(diffs) == 0 {
		return nil
	}
	return errors.New(strings.Join(diffs, ", "))
}

func (s *Server) handleSymbolicRefUpdate(ctx context.Context, logger log.Logger, sender *serverConn, repo *serverRepo, ref refdb.OwnedRef, newTargetRef *refdb.OwnedRef, params RefUpdateSymbolicParams) error {
	CheckSymbolicRefName(params.RefIdentifier.Ref)

	logger = log.With(logger, "update-symbolic-ref", params.RefIdentifier.Ref, "old", params.OldTarget, "new", params.Target)
	level.Info(logger).Log()

	timer := time.AfterFunc(5*time.Second, func() {
		level.Warn(logger).Log("delay", "taking a long time, possible deadlock")
	})
	defer timer.Stop()

	if newTargetRef == nil {
		tmp := repo.refdb.Lookup(params.Target)
		newTargetRef = &tmp
		defer newTargetRef.Unlock()
	}
	if newTargetRef.Ref == nil {
		return &jsonrpc2.Error{
			Code:    int64(ErrorCodeRefNotExists),
			Message: fmt.Sprintf("update of symbolic ref %q to nonexistent ref %q", params.RefIdentifier.Ref, params.Target),
		}
	}
	if newTargetRef.Ref.IsSymbolic() {
		return &jsonrpc2.Error{
			Code:    int64(ErrorCodeSymbolicRefInvalid),
			Message: fmt.Sprintf("invalid update of symbolic ref %q target to symbolic ref %q (must be non-symbolic ref)", params.RefIdentifier.Ref, params.Target),
		}
	}

	debugutil.SimulateLatency()

	var old *refdb.Ref
	if params.OldTarget != "" {
		old = &refdb.Ref{Name: params.RefIdentifier.Ref, Target: params.OldTarget}
	}
	if err := repo.refdb.CompareAndWrite(refdb.Ref{Name: params.RefIdentifier.Ref, Target: params.Target}, old); err != nil {
		if _, ok := err.(*refdb.WrongOldRefValueError); ok {
			return &jsonrpc2.Error{
				Code:    int64(ErrorCodeRefUpdateInvalid),
				Message: err.Error(),
			}
		}
		return err
	}

	return s.broadcastRefUpdate(ctx, logger, sender, nil, &params)
}

func (s *Server) handleRefUpdateFromDownstream(ctx context.Context, logger log.Logger, repo *serverRepo, ref refdb.OwnedRef, params RefUpdateUpstreamParams, sender *serverConn, applyLocally bool) error {
	CheckRefName(params.RefIdentifier.Ref)

	if err := params.validate(); err != nil {
		return &jsonrpc2.Error{
			Code:    jsonrpc2.CodeInvalidParams,
			Message: "invalid params for ref update from downstream: " + err.Error(),
		}
	}

	if strings.HasPrefix(params.RefIdentifier.Ref, "remote/") {
		return &jsonrpc2.Error{
			Code:    int64(ErrorCodeRefUpdateInvalid),
			Message: fmt.Sprintf("remote tracking ref %q cannot be updated by a downstream (only by the upstream remote it tracks)", params.RefIdentifier.Ref),
		}
	}

	if sender != nil {
		logger = log.With(logger, "update-ref-from-downstream", params.RefIdentifier.Ref)
	} else {
		logger = log.With(logger, "update-ref-locally", params.RefIdentifier.Ref)
	}
	logger = log.With(logger, "params", params)
	level.Info(logger).Log("apply-locally", applyLocally)

	timer := time.AfterFunc(5*time.Second, func() {
		level.Warn(logger).Log("delay", "taking a long time, possible deadlock")
	})
	defer timer.Stop()

	if ref.Ref != nil && ref.Ref.IsSymbolic() && !params.Force {
		return &jsonrpc2.Error{
			Code:    int64(ErrorCodeRefUpdateInvalid),
			Message: fmt.Sprintf("a force-update is required to overwrite symbolic ref %q with a non-symbolic ref", params.RefIdentifier.Ref),
		}
	}

	debugutil.SimulateLatency()

	if params.Delete {
		// Delete ref.
		if ref.Ref == nil {
			return &jsonrpc2.Error{
				Code:    int64(ErrorCodeRefNotExists),
				Message: fmt.Sprintf("downstream sent ref deletion for nonexistent ref %q", params.RefIdentifier.Ref),
			}
		}
		if err := compareRefPointerInfo(*params.Current, ref.Ref.Object.(serverRef)); err != nil {
			return &jsonrpc2.Error{
				Code:    int64(ErrorCodeRefConflict),
				Message: fmt.Sprintf("downstream sent ref deletion with invalid current info: %s", err),
			}
		}
		if err := repo.refdb.Delete(ref); err != nil {
			return err
		}
	} else {
		// Create or update ref.
		if params.Current == nil {
			if ref.Ref != nil && !params.Force {
				return &jsonrpc2.Error{
					Code:    int64(ErrorCodeRefExists),
					Message: fmt.Sprintf("downstream sent ref update for existing ref %q, but neither current nor force was set on the update", params.RefIdentifier.Ref),
				}
			}
			ref.Ref = &refdb.Ref{Name: params.RefIdentifier.Ref, Object: serverRef{}}
		}
		if params.Current != nil {
			if ref.Ref == nil {
				return &jsonrpc2.Error{
					Code:    int64(ErrorCodeRefNotExists),
					Message: fmt.Sprintf("downstream sent ref update for nonexistent ref %q", params.RefIdentifier.Ref),
				}
			}
		}

		refObj := ref.Ref.Object.(serverRef)

		if params.Current != nil {
			if err := compareRefBaseInfo(params.Current.RefBaseInfo, ref.Ref.Object.(serverRef)); err != nil {
				return &jsonrpc2.Error{
					Code:    int64(ErrorCodeRefConflict),
					Message: fmt.Sprintf("downstream sent ref update with invalid current info: %s", err),
				}
			}
		}

		switch {
		case params.State != nil:
			// Propagate a non-op-only change upstream; otherwise we
			// will just append to the upstream's ref op history and
			// not actually reset it.
			repoConfig, err := repo.getConfig()
			if err != nil {
				return err
			}
			if refConfig, ok := repoConfig.Refs[params.RefIdentifier.Ref]; ok && refConfig.Overwrite {
				remote, ok := repoConfig.Remotes[refConfig.Upstream]
				if !ok {
					return &jsonrpc2.Error{
						Code:    int64(ErrorCodeRemoteNotExists),
						Message: fmt.Sprintf("upstream remote %q configured for ref %s does not exist", refConfig.Upstream, params.RefIdentifier),
					}
				}
				cl, err := s.remotes.getOrCreateClient(ctx, logger, remote.Endpoint)
				if err != nil {
					return err
				}
				upstreamParams := RefUpdateUpstreamParams{
					RefIdentifier: RefIdentifier{
						Repo: remote.Repo,
						Ref:  params.RefIdentifier.Ref,
					},
					Force: params.Force || refConfig.Overwrite,
					State: params.State,
				}
				// Only set Current if Force is false, or else the
				// server will complain that the update is invalid.
				if !upstreamParams.Force {
					upstreamParams.Current = params.Current
				}
				if err := cl.RefUpdate(ctx, upstreamParams); err != nil {
					return err
				}
			}

			var otHandler *ot.Proxy
			if head := repo.refdb.LookupShared("HEAD"); head != nil && head.Target == params.RefIdentifier.Ref {
				repo.mu.Lock()
				workspace := repo.workspace
				repo.mu.Unlock()
				if applyLocally {
					if _, err := workspace.Checkout(ctx, logger, false, params.RefIdentifier.Ref, params.State.GitBase, params.State.GitBranch, params.State.History, nil); err != nil {
						return err
					}
					applyLocally = false // just did apply locally, don't do it again below
				}
				otHandler = &ot.Proxy{
					Apply: func(logger log.Logger, op ot.WorkspaceOp) error {
						return workspace.Apply(ctx, logger, op)
					},
				}
			} else {
				otHandler, err = s.Backend.Create(ctx, logger, params.RefIdentifier.Repo, params.State.GitBase)
				if err != nil {
					return err
				}
				if prevOT := refObj.ot; prevOT != nil {
					if otHandler.Apply == nil && prevOT.Apply != nil {
						// TODO(sqs): this is hacky, mainly for when our
						// mock tests have set an Apply and we want to
						// reuse it
						otHandler.Apply = prevOT.Apply
						level.Warn(logger).Log("HACK-used-prev-ot-handler-Apply-func", "")
					}
				}
			}

			if prevOT := refObj.ot; prevOT != nil {
				if otHandler.SendToUpstream != nil {
					// This should never happen, but just be safe.
					panic(fmt.Sprintf("new otHandler from backend %T already has SendToUpstream func", s.Backend))
				}
				otHandler.SendToUpstream = prevOT.SendToUpstream
			}

			if len(params.State.History) > 0 {
				if applyLocally && otHandler.Apply != nil {
					// Compose them into 1 so we perform fewer Git
					// operations. The outcome is the same as applying
					// them serially.
					composed, err := ot.ComposeAllWorkspaceOps(params.State.History)
					if err != nil {
						return err
					}
					if err := otHandler.Apply(logger, composed); err != nil {
						return err
					}
				}

				for _, op := range params.State.History {
					if err := otHandler.Record(logger, op); err != nil {
						return err
					}
				}
			}

			otHandler.UpstreamRevNumber = len(params.State.History)
			ref.Ref.Object = serverRef{
				gitBase:   params.State.GitBase,
				gitBranch: params.State.GitBranch,
				ot:        otHandler,
			}

		case params.Op != nil:
			if xop, err := refObj.ot.RecvFromDownstream(logger, params.Current.Rev, *params.Op); err == nil {
				params.Op = &xop
			} else {
				return &jsonrpc2.Error{
					Code:    int64(ErrorCodeInvalidOp),
					Message: err.Error(),
				}
			}
			debugutil.SimulateLatency()
		}

		if err := repo.refdb.Write(ref); err != nil {
			return err
		}

		// If we previously configured this ref to have an
		// upstream BEFORE this ref existed, then we need to check
		// now if we need to link the upstream to it.
		hasUpstreamConfigured := refObj.ot != nil && refObj.ot.SendToUpstream != nil
		if !hasUpstreamConfigured {
			repoConfig, err := repo.getConfig()
			if err != nil {
				return err
			}
			if c, ok := repoConfig.Refs[params.RefIdentifier.Ref]; ok {
				level.Info(logger).Log("reattaching-ref-config-to-newly-created-ref", fmt.Sprint(c))
				if err := s.doApplyRefConfiguration(ctx, logger, repo, params.RefIdentifier, ref, repoConfig, repoConfig, true, false); err != nil {
					return err
				}
			}
		}
	}

	toRefBaseInfo := func(p *RefPointer) *RefBaseInfo {
		if p == nil {
			return nil
		}
		return &RefBaseInfo{GitBase: p.GitBase, GitBranch: p.GitBranch}
	}
	return s.broadcastRefUpdate(ctx, logger, sender, &RefUpdateDownstreamParams{
		RefIdentifier: params.RefIdentifier,
		Current:       toRefBaseInfo(params.Current),
		State:         params.State,
		Op:            params.Op,
		Delete:        params.Delete,
	}, nil)
}

func clientIDs(conns []*serverConn) (ids []string) {
	ids = make([]string, len(conns))
	for i, c := range conns {
		c.mu.Lock()
		if c.init != nil {
			ids[i] = c.init.ID
		}
		c.mu.Unlock()
	}
	sort.Strings(ids)
	return ids
}

type sortableRefs []refdb.Ref

func (v sortableRefs) Len() int           { return len(v) }
func (v sortableRefs) Swap(i, j int)      { v[i], v[j] = v[j], v[i] }
func (v sortableRefs) Less(i, j int) bool { return v[i].Name < v[j].Name }
