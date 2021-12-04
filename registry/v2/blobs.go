package registry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/containerish/OpenRegistry/types"
	"github.com/fatih/color"
	"github.com/labstack/echo/v4"
)

func (b *blobs) errorResponse(code, msg string, detail map[string]interface{}) []byte {
	var err RegistryErrors

	err.Errors = append(err.Errors, RegistryError{
		Code:    code,
		Message: msg,
		Detail:  detail,
	})

	bz, e := json.Marshal(err)
	if e != nil {
		color.Red("blob error: %s", e.Error())
		return []byte{}
	}

	return bz
}

func (b *blobs) HEAD(ctx echo.Context) error {
	ctx.Set(types.HandlerStartTime, time.Now())
	defer func() {
		b.registry.logger.Log(ctx).Send()
	}()

	digest := ctx.Param("digest")
	layerRef, err := b.registry.localCache.GetDigest(digest)
	if err != nil {
		details := echo.Map{
			"skynet": "skynet link not found",
		}
		errMsg := b.errorResponse(RegistryErrorCodeManifestBlobUnknown, err.Error(), details)

		ctx.Set(types.HttpEndpointErrorKey, errMsg)
		return ctx.JSONBlob(http.StatusNotFound, errMsg)
	}

	size, ok := b.registry.skynet.Metadata(layerRef.Skylink)
	if !ok {
		errMsg := b.errorResponse(RegistryErrorCodeManifestBlobUnknown, "Manifest does not exist", nil)
		ctx.Set(types.HttpEndpointErrorKey, errMsg)
		return ctx.JSONBlob(http.StatusNotFound, errMsg)
	}

	ctx.Response().Header().Set("Content-Length", fmt.Sprintf("%d", size))
	ctx.Response().Header().Set("Docker-Content-Digest", digest)
	return ctx.String(http.StatusOK, "OK")
}

/*
UploadBlob
for postgres
insert into blob table one blob at a time
these will be part of the txn in StartUpload
*/
func (b *blobs) UploadBlob(ctx echo.Context) error {
	ctx.Set(types.HandlerStartTime, time.Now())
	defer func() {
		b.registry.logger.Log(ctx).Send()
	}()

	namespace := ctx.Param("username") + "/" + ctx.Param("imagename")
	contentRange := ctx.Request().Header.Get("Content-Range")
	uuid := ctx.Param("uuid")

	if contentRange == "" {
		if _, ok := b.uploads[uuid]; ok {
			errMsg := b.errorResponse(
				RegistryErrorCodeBlobUploadInvalid,
				"stream upload after first write are not allowed",
				nil,
			)
			ctx.Set(types.HttpEndpointErrorKey, errMsg)

			return ctx.JSONBlob(http.StatusBadRequest, errMsg)
		}

		bz, _ := io.ReadAll(ctx.Request().Body)
		defer ctx.Request().Body.Close()

		b.uploads[uuid] = bz

		if err := b.blobTransaction(ctx, bz, uuid); err != nil {
			errMsg := b.errorResponse(
				RegistryErrorCodeBlobUploadInvalid,
				err.Error(),
				nil,
			)
			ctx.Set(types.HttpEndpointErrorKey, errMsg)
			return ctx.JSONBlob(http.StatusBadRequest, errMsg)
		}

		locationHeader := fmt.Sprintf("/v2/%s/blobs/uploads/%s", namespace, uuid)
		ctx.Response().Header().Set("Location", locationHeader)
		ctx.Response().Header().Set("Range", fmt.Sprintf("0-%d", len(bz)-1))
		return ctx.NoContent(http.StatusAccepted)
	}

	start, end := 0, 0
	// 0-90
	if _, err := fmt.Sscanf(contentRange, "%d-%d", &start, &end); err != nil {
		details := map[string]interface{}{
			"error":        "content range is invalid",
			"contentRange": contentRange,
		}
		errMsg := b.errorResponse(RegistryErrorCodeBlobUploadUnknown, err.Error(), details)
		ctx.Set(types.HttpEndpointErrorKey, errMsg)
		return ctx.JSONBlob(http.StatusRequestedRangeNotSatisfiable, errMsg)
	}

	if start != len(b.uploads[uuid]) {
		errMsg := b.errorResponse(RegistryErrorCodeBlobUploadUnknown, "content range mismatch", nil)
		ctx.Set(types.HttpEndpointErrorKey, errMsg)
		return ctx.JSONBlob(http.StatusRequestedRangeNotSatisfiable, errMsg)
	}

	buf := bytes.NewBuffer(b.uploads[uuid]) // 90
	_, err := io.Copy(buf, ctx.Request().Body)
	if err != nil {
		errMsg := b.errorResponse(
			RegistryErrorCodeBlobUploadInvalid,
			"error while creating new buffer from existing blobs",
			nil,
		)
		ctx.Set(types.HttpEndpointErrorKey, errMsg)
		return ctx.JSONBlob(http.StatusInternalServerError, errMsg)
	} // 10
	ctx.Request().Body.Close()

	b.uploads[uuid] = buf.Bytes()
	if err := b.blobTransaction(ctx, buf.Bytes(), uuid); err != nil {
		errMsg := b.errorResponse(
			RegistryErrorCodeBlobUploadInvalid,
			err.Error(),
			nil,
		)
		ctx.Set(types.HttpEndpointErrorKey, errMsg)
		return ctx.JSONBlob(http.StatusBadRequest, errMsg)
	}
	locationHeader := fmt.Sprintf("/v2/%s/blobs/uploads/%s", namespace, uuid)
	ctx.Response().Header().Set("Location", locationHeader)
	ctx.Response().Header().Set("Range", fmt.Sprintf("0-%d", buf.Len()-1))
	return ctx.NoContent(http.StatusAccepted)
}

func (b *blobs) blobTransaction(ctx echo.Context, bz []byte, uuid string) error {
	blob := &types.Blob{
		Digest:     digest(bz),
		Skylink:    "",
		UUID:       uuid,
		RangeStart: 0,
		RangeEnd:   uint32(len(bz) - 1),
	}

	txnOp, ok := b.registry.txnMap[uuid]
	if !ok {
		return fmt.Errorf("txn has not been initialised for uuid - " + uuid)
	}

	if err := b.registry.store.SetBlob(ctx.Request().Context(), txnOp.txn, blob); err != nil {
		return b.registry.store.Abort(ctx.Request().Context(), txnOp.txn)
	}

	txnOp.blobDigests = append(txnOp.blobDigests, blob.Digest)
	b.registry.txnMap[uuid] = txnOp
	return nil
}
