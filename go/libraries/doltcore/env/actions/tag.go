// Copyright 2020 Liquidata, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package actions

import (
	"context"
	"fmt"
	"sort"

	"github.com/liquidata-inc/dolt/go/libraries/doltcore/doltdb"
	"github.com/liquidata-inc/dolt/go/libraries/doltcore/env"
	"github.com/liquidata-inc/dolt/go/libraries/doltcore/ref"
)

type TagProps struct {
	TaggerName  string
	TaggerEmail string
	Description string
}

func CreateTag(ctx context.Context, dEnv *env.DoltEnv, tagName, startPoint string, props TagProps) error {
	tagRef := ref.NewTagRef(tagName)

	hasRef, err := dEnv.DoltDB.HasRef(ctx, tagRef)

	if err != nil {
		return err
	}

	if hasRef {
		return ErrAlreadyExists
	}

	if !ref.IsValidTagName(tagName) {
		return doltdb.ErrInvTagName
	}

	cs, err := doltdb.NewCommitSpec(startPoint)

	if err != nil {
		return err
	}

	cm, err := dEnv.DoltDB.Resolve(ctx, cs, dEnv.RepoState.CWBHeadRef())

	if err != nil {
		return err
	}

	meta := doltdb.NewTagMeta(props.TaggerName, props.TaggerEmail, props.Description)

	return dEnv.DoltDB.NewTagAtCommit(ctx, tagRef, cm, meta)
}

func DeleteTags(ctx context.Context, dEnv *env.DoltEnv, tagNames ...string) error {
	for _, tn := range tagNames {
		dref := ref.NewTagRef(tn)

		hasRef, err := dEnv.DoltDB.HasRef(ctx, dref)

		if err != nil {
			return err
		}
		if !hasRef {
			return doltdb.ErrTagNotFound
		}

		err = dEnv.DoltDB.DeleteTag(ctx, dref)

		if err != nil {
			return err
		}
	}
	return nil
}

type resolvedTag struct {
	tag    ref.DoltRef
	commit *doltdb.Commit
	meta   *doltdb.TagMeta
}

// IterResolvedTags iterates over tags in dEnv.DoltDB from newest to oldest, resolving the tag to a commit and calling cb().
func IterResolvedTags(ctx context.Context, ddb *doltdb.DoltDB, cb func(tag ref.DoltRef, c *doltdb.Commit, meta *doltdb.TagMeta) (stop bool, err error)) error {
	tagRefs, err := ddb.GetTags(ctx)

	if err != nil {
		return err
	}

	var resolved []resolvedTag
	for _, r := range tagRefs {
		tag, ok := r.(ref.TagRef)
		if !ok {
			return fmt.Errorf("DoltDB.GetTags() returned non-tag DoltRef")
		}

		commit, meta, err := ddb.ResolveTag(ctx, tag)
		if err != nil {
			return err
		}

		resolved = append(resolved, resolvedTag{
			tag:    tag,
			commit: commit,
			meta:   meta,
		})
	}

	// iterate newest to oldest
	sort.Slice(resolved, func(i, j int) bool {
		return resolved[i].meta.Timestamp > resolved[j].meta.Timestamp
	})

	for _, st := range resolved {
		stop, err := cb(st.tag, st.commit, st.meta)

		if err != nil {
			return err
		}
		if stop {
			break
		}
	}

	return nil
}
