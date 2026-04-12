package coretools

import "github.com/codalotl/codalotl/internal/llmstream"

func (t *toolApplyPatch) Presenter() llmstream.Presenter {
	return nil
}

func (t *toolDelete) Presenter() llmstream.Presenter {
	return deletePresenterInstance
}

func (t *toolEdit) Presenter() llmstream.Presenter {
	return nil
}

func (t *toolLs) Presenter() llmstream.Presenter {
	return lsPresenterInstance
}

func (t *toolReadFile) Presenter() llmstream.Presenter {
	return readFilePresenterInstance
}

func (t *toolShell) Presenter() llmstream.Presenter {
	return shellPresenterInstance
}

func (t *toolSkillShell) Presenter() llmstream.Presenter {
	return shellPresenterInstance
}

func (t *toolUpdatePlan) Presenter() llmstream.Presenter {
	return updatePlanPresenterInstance
}

func (t *toolWrite) Presenter() llmstream.Presenter {
	return nil
}
