package store

import (
	"context"

	"footprint-serverful/ent"
	"footprint-serverful/ent/project"
	"footprint-serverful/internal/model"
)

func (s *Store) FindProject(ctx context.Context, name string) (model.Project, error) {
	return findProject(ctx, s.client, name)
}

func findProject(ctx context.Context, client *ent.Client, name string) (model.Project, error) {
	p, err := client.Project.Query().Where(project.NameEQ(name)).Order(ent.Asc(project.FieldID)).First(ctx)
	if ent.IsNotFound(err) {
		return model.Project{}, ErrProjectNotFound
	}
	if err != nil {
		return model.Project{}, err
	}
	return projectJSON(p), nil
}

func (s *Store) ListProjects(ctx context.Context, limit, offset int) (model.ProjectPage, error) {
	rows, err := s.client.Project.Query().Order(ent.Desc(project.FieldStartTime, project.FieldID)).
		Limit(limit + 1).Offset(offset).All(ctx)
	if err != nil {
		return model.ProjectPage{}, err
	}
	result := model.ProjectPage{Projects: []model.Project{}, Pagination: model.Pagination{
		Limit: limit, Offset: offset, HasMore: len(rows) > limit,
	}}
	for _, row := range rows[:min(len(rows), limit)] {
		result.Projects = append(result.Projects, projectJSON(row))
	}
	return result, nil
}

func (s *Store) CreateProject(ctx context.Context, input model.ProjectInput) (model.Project, error) {
	if input.EndTime < input.StartTime {
		return model.Project{}, ErrProjectRange
	}
	p, err := s.client.Project.Create().SetName(input.Name).SetStartTime(input.StartTime).SetEndTime(input.EndTime).Save(ctx)
	if err != nil {
		return model.Project{}, err
	}
	return projectJSON(p), nil
}

func (s *Store) UpdateProject(ctx context.Context, id int64, input model.ProjectPatch) (model.Project, error) {
	return withTx(ctx, s.client, nil, func(tx *ent.Tx) (model.Project, error) {
		p, err := tx.Project.Query().Where(project.IDEQ(id)).ForUpdate().Only(ctx)
		if ent.IsNotFound(err) {
			return model.Project{}, ErrProjectNotFound
		}
		if err != nil {
			return model.Project{}, err
		}
		if input.Name != nil {
			p.Name = *input.Name
		}
		if input.StartTime != nil {
			p.StartTime = *input.StartTime
		}
		if input.EndTime != nil {
			p.EndTime = *input.EndTime
		}
		if p.StartTime > p.EndTime {
			return model.Project{}, ErrProjectRange
		}
		saved, err := tx.Project.UpdateOneID(id).SetName(p.Name).SetStartTime(p.StartTime).SetEndTime(p.EndTime).Save(ctx)
		if err != nil {
			return model.Project{}, err
		}
		return projectJSON(saved), nil
	})
}
