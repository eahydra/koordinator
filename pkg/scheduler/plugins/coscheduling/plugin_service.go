package coscheduling

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/koordinator-sh/koordinator/pkg/scheduler/frameworkext/services"
)

var _ services.APIServiceProvider = &Coscheduling{}

func (cs *Coscheduling) RegisterEndpoints(group *gin.RouterGroup) {
	group.GET("/gang/:namespace/:name", func(c *gin.Context) {
		gangNamespace := c.Param("namespace")
		gangName := c.Param("name")
		gangFullName := fmt.Sprintf("%s/%s", gangNamespace, gangName)
		gangSummary, exist := cs.pgMgr.GetGangSummary(gangFullName)
		if !exist {
			services.ResponseErrorMessage(c, http.StatusNotFound, "cannot find gang %s/%s", gangNamespace, gangName)
			return
		}
		c.JSON(http.StatusOK, gangSummary)
	})
	group.GET("/gangs", func(c *gin.Context) {
		allGangSummaries := cs.pgMgr.GetGangSummaries()
		c.JSON(http.StatusOK, allGangSummaries)
	})
}
