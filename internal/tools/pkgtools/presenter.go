package pkgtools

import "github.com/codalotl/codalotl/internal/llmstream"

func (t *toolChangeAPI) Presenter() llmstream.Presenter {
	return llmstream.NewAppendToolPresenter()
}

func (t *toolClarifyPublicAPI) Presenter() llmstream.Presenter {
	return llmstream.NewAppendToolPresenter()
}

func (t *toolGetPublicAPI) Presenter() llmstream.Presenter {
	return llmstream.NewDefaultToolPresenter()
}

func (t *toolGetUsage) Presenter() llmstream.Presenter {
	return llmstream.NewDefaultToolPresenter()
}

func (t *toolModuleInfo) Presenter() llmstream.Presenter {
	return llmstream.NewDefaultToolPresenter()
}

func (t *toolUpdateUsage) Presenter() llmstream.Presenter {
	return llmstream.NewAppendToolPresenter()
}
