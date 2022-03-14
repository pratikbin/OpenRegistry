package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/containerish/OpenRegistry/cache"
	"github.com/containerish/OpenRegistry/skynet"
	"github.com/containerish/OpenRegistry/store/postgres"
	"github.com/containerish/OpenRegistry/telemetry"
	"github.com/containerish/OpenRegistry/types"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

func NewRegistry(
	skynetClient *skynet.Client,
	c cache.Store,
	logger telemetry.Logger,
	pgStore postgres.PersistentStore,
) (Registry, error) {
	r := &registry{
		debug:  true,
		skynet: skynetClient,
		b: blobs{
			mutex:    sync.Mutex{},
			contents: map[string][]byte{},
			uploads:  map[string][]byte{},
			layers:   map[string][]string{},
		},
		localCache: c,
		logger:     logger,
		mu:         &sync.RWMutex{},
		store:      pgStore,
		txnMap:     map[string]TxnStore{},
	}

	r.b.registry = r

	return r, nil
}

// LayerExists
// HEAD /v2/<name>/blobs/<digest>
// 200 OK
// Content-Length: <length of blob>
// Docker-Content-Digest: <digest>
// OK
func (r *registry) LayerExists(ctx echo.Context) error {
	return r.b.HEAD(ctx)
}

// ManifestExists
// HEAD /v2/<name>/manifests/<reference>
// OK
func (r *registry) ManifestExists(ctx echo.Context) error {
	ctx.Set(types.HandlerStartTime, time.Now())

	namespace := ctx.Param("username") + "/" + ctx.Param("imagename")
	ref := ctx.Param("reference") // ref can be either tag or digest

	manifest, err := r.store.GetManifestByReference(ctx.Request().Context(), namespace, ref)

	if err != nil {
		details := echo.Map{
			"skynet": "manifest not found",
			"error":  err.Error(),
		}

		errMsg := r.errorResponse(RegistryErrorCodeManifestBlobUnknown, err.Error(), details)
		ctx.Set(types.HttpEndpointErrorKey, errMsg)
		r.logger.Log(ctx)
		return ctx.JSONBlob(http.StatusNotFound, errMsg)
	}

	metadata, err := r.skynet.Metadata(manifest.Skylink)
	if err != nil {
		detail := map[string]interface{}{
			"error":   err.Error(),
			"skylink": manifest.Skylink,
		}

		errMsg := r.errorResponse(RegistryErrorCodeManifestBlobUnknown, "Manifest does not exist", detail)
		ctx.Set(types.HttpEndpointErrorKey, errMsg)
		r.logger.Log(ctx)

		return ctx.JSONBlob(http.StatusNotFound, errMsg)
	}

	if manifest.Reference != ref && manifest.Digest != ref {
		details := map[string]interface{}{
			"foundDigest":  manifest.Digest,
			"clientDigest": ref,
		}
		r.logger.Error(details)
		errMsg := r.errorResponse(RegistryErrorCodeManifestInvalid, "manifest digest does not match", nil)
		ctx.Set(types.HttpEndpointErrorKey, errMsg)
		r.logger.Log(ctx)

		return ctx.JSONBlob(http.StatusBadRequest, errMsg)
	}

	ctx.Response().Header().Set("Content-Type", "application/json")
	ctx.Response().Header().Set("Content-Length", fmt.Sprintf("%d", metadata.ContentLength))
	ctx.Response().Header().Set("Docker-Content-Digest", manifest.Digest)

	return ctx.NoContent(http.StatusOK)
}

// Catalog - The list of available repositories is made available through the catalog.
// GET /v2/_catalog?n=10&last=10&ns=johndoe
// OK
func (r *registry) Catalog(ctx echo.Context) error {
	ctx.Set(types.HandlerStartTime, time.Now())

	queryParamPageSize := ctx.QueryParam("n")
	queryParamOffset := ctx.QueryParam("last")
	namespace := ctx.QueryParam("ns")
	var pageSize int64
	var offset int64
	if queryParamPageSize != "" {
		ps, err := strconv.ParseInt(ctx.QueryParam("n"), 10, 64)
		if err != nil {
			ctx.Set(types.HttpEndpointErrorKey, err.Error())
			r.logger.Log(ctx)
			return ctx.JSON(http.StatusBadRequest, echo.Map{
				"error": err.Error(),
			})
		}
		pageSize = ps
	}

	if queryParamOffset != "" {
		o, err := strconv.ParseInt(ctx.QueryParam("last"), 10, 64)
		if err != nil {
			ctx.Set(types.HttpEndpointErrorKey, err.Error())
			r.logger.Log(ctx)
			return ctx.JSON(http.StatusBadRequest, echo.Map{
				"error": err.Error(),
			})
		}
		offset = o
	}

	catalogList, err := r.store.GetCatalog(ctx.Request().Context(), namespace, pageSize, offset)
	if err != nil {
		ctx.Set(types.HttpEndpointErrorKey, err.Error())
		r.logger.Log(ctx)
		return ctx.JSON(http.StatusInternalServerError, echo.Map{
			"error": err.Error(),
		})
	}
	total, err := r.store.GetCatalogCount(ctx.Request().Context())
	if err != nil {
		ctx.Set(types.HttpEndpointErrorKey, err.Error())
		r.logger.Log(ctx)
		return ctx.JSON(http.StatusInternalServerError, echo.Map{
			"error": err.Error(),
		})
	}
	return ctx.JSON(http.StatusOK, echo.Map{
		"repositories": catalogList,
		"total":        total,
	})

}

// ListTags Content discovery
// GET /v2/<name>/tags/list
// OK
func (r *registry) ListTags(ctx echo.Context) error {
	ctx.Set(types.HandlerStartTime, time.Now())

	namespace := ctx.Param("username") + "/" + ctx.Param("imagename")
	limit := ctx.QueryParam("n")

	tags, err := r.store.GetImageTags(ctx.Request().Context(), namespace)
	if err != nil {
		errMsg := r.errorResponse(RegistryErrorCodeTagInvalid, err.Error(), nil)
		ctx.Set(types.HttpEndpointErrorKey, errMsg)
		r.logger.Log(ctx)
		return ctx.JSONBlob(http.StatusNotFound, errMsg)
	}

	if limit != "" {
		n, err := strconv.ParseInt(limit, 10, 32)
		if err != nil {
			errMsg := r.errorResponse(RegistryErrorCodeTagInvalid, err.Error(), nil)
			ctx.Set(types.HttpEndpointErrorKey, errMsg)
			r.logger.Log(ctx)
			return ctx.JSONBlob(http.StatusNotFound, errMsg)
		}
		if n > 0 {
			tags = tags[0:n]
		}
		if n == 0 {
			tags = nil
		}
	}

	return ctx.JSON(http.StatusOK, echo.Map{
		"name": namespace,
		"tags": tags,
	})
}
func (r *registry) List(ctx echo.Context) error {
	return fmt.Errorf("error")
}

// PullManifest
// GET /v2/<name>/manifests/<reference>
// OK
func (r *registry) PullManifest(ctx echo.Context) error {
	ctx.Set(types.HandlerStartTime, time.Now())

	namespace := ctx.Param("username") + "/" + ctx.Param("imagename")
	ref := ctx.Param("reference")

	manifest, err := r.store.GetManifestByReference(ctx.Request().Context(), namespace, ref)
	if err != nil {
		errMsg := r.errorResponse(RegistryErrorCodeManifestUnknown, err.Error(), nil)
		ctx.Set(types.HttpEndpointErrorKey, errMsg)
		r.logger.Log(ctx)
		return ctx.JSONBlob(http.StatusNotFound, errMsg)
	}
	resp, err := r.skynet.Download(manifest.Skylink)
	if err != nil {
		errMsg := r.errorResponse(RegistryErrorCodeManifestInvalid, err.Error(), nil)
		ctx.Set(types.HttpEndpointErrorKey, errMsg)
		r.logger.Log(ctx)
		return ctx.JSONBlob(http.StatusNotFound, errMsg)
	}

	bz, err := io.ReadAll(resp)
	if err != nil {
		errMsg := r.errorResponse(RegistryErrorCodeManifestInvalid, err.Error(), nil)
		ctx.Set(types.HttpEndpointErrorKey, errMsg)
		r.logger.Log(ctx)
		return ctx.JSONBlob(http.StatusNotFound, errMsg)
	}
	_ = resp.Close()
	ctx.Response().Header().Set("Docker-Content-Digest", manifest.Digest)
	ctx.Response().Header().Set("X-Docker-Content-ID", manifest.Skylink)
	ctx.Response().Header().Set("Content-Type", manifest.MediaType)
	ctx.Response().Header().Set("Content-Length", fmt.Sprintf("%d", len(bz)))
	return ctx.JSONBlob(http.StatusOK, bz)
}

// PullLayer
// GET /v2/<name>/blobs/<digest>
// OK, error: binary output can mess your system ...
func (r *registry) PullLayer(ctx echo.Context) error {
	//namespace := ctx.Param("username") + "/" + ctx.Param("imagename")
	ctx.Set(types.HandlerStartTime, time.Now())

	clientDigest := ctx.Param("digest")

	layer, err := r.store.GetLayer(ctx.Request().Context(), clientDigest)
	if err != nil {
		errMsg := r.errorResponse(RegistryErrorCodeBlobUnknown, err.Error(), nil)
		ctx.Set(types.HttpEndpointErrorKey, errMsg)
		r.logger.Log(ctx)
		return ctx.JSONBlob(http.StatusNotFound, errMsg)

	}

	if layer.SkynetLink == "" {
		detail := map[string]interface{}{
			"error": "skylink is empty",
		}
		e := fmt.Errorf("skylink is empty").Error()
		errMsg := r.errorResponse(RegistryErrorCodeBlobUnknown, e, detail)
		ctx.Set(types.HttpEndpointErrorKey, errMsg)
		r.logger.Log(ctx)
		return ctx.JSONBlob(http.StatusNotFound, errMsg)
	}

	resp, err := r.skynet.Download(layer.SkynetLink)
	if err != nil {
		detail := map[string]interface{}{
			"error":   err.Error(),
			"skylink": layer.SkynetLink,
		}
		errMsg := r.errorResponse(RegistryErrorCodeBlobUnknown, err.Error(), detail)
		ctx.Set(types.HttpEndpointErrorKey, errMsg)
		r.logger.Log(ctx)
		return ctx.JSONBlob(http.StatusNotFound, errMsg)
	}
	buf := &bytes.Buffer{}
	if _, err := io.Copy(buf, resp); err != nil {
		errMsg := r.errorResponse(RegistryErrorCodeBlobUploadInvalid, err.Error(), nil)
		ctx.Set(types.HttpEndpointErrorKey, errMsg)
		r.logger.Log(ctx)
		return ctx.JSONBlob(http.StatusInternalServerError, errMsg)
	}
	_ = resp.Close()

	dig := digest(buf.Bytes())
	if dig != clientDigest {
		details := map[string]interface{}{
			"clientDigest":   clientDigest,
			"computedDigest": dig,
		}
		errMsg := r.errorResponse(
			RegistryErrorCodeBlobUploadUnknown,
			"client digest is different than computed digest",
			details,
		)
		ctx.Set(types.HttpEndpointErrorKey, errMsg)
		r.logger.Log(ctx)
		return ctx.JSONBlob(http.StatusNotFound, errMsg)
	}

	ctx.Response().Header().Set("Content-Length", fmt.Sprintf("%d", len(buf.Bytes())))
	ctx.Response().Header().Set("Docker-Content-Digest", dig)
	return ctx.Blob(http.StatusOK, "application/octet-stream", buf.Bytes())
}

// MonolithicUpload
// PUT /v2/<name>/blobs/uploads/<uuid>?digest=<digest>
func (r *registry) MonolithicUpload(ctx echo.Context) error {
	ctx.Set(types.HandlerStartTime, time.Now())

	namespace := ctx.Param("username") + "/" + ctx.Param("imagename")
	uuid := ctx.Param("uuid")
	digest := ctx.QueryParam("digest")

	buf := &bytes.Buffer{}
	if _, err := io.Copy(buf, ctx.Request().Body); err != nil {
		errMsg := r.errorResponse(RegistryErrorCodeBlobUploadInvalid, err.Error(), nil)
		ctx.Set(types.HttpEndpointErrorKey, errMsg)
		r.logger.Log(ctx)
		return ctx.JSONBlob(http.StatusBadRequest, errMsg)
	}
	_ = ctx.Request().Body.Close()

	link, err := r.skynet.Upload(namespace, digest, buf.Bytes(), true)
	if err != nil {
		detail := echo.Map{
			"error":  err.Error(),
			"caller": "MonolithicUpload",
		}
		errMsg := r.errorResponse(RegistryErrorCodeBlobUploadInvalid, err.Error(), detail)
		ctx.Set(types.HttpEndpointErrorKey, errMsg)
		r.logger.Log(ctx)
		return ctx.JSONBlob(http.StatusInternalServerError, buf.Bytes())
	}

	metadata := types.Metadata{
		Namespace: namespace,
		Manifest: types.ImageManifest{
			SchemaVersion: 2,
			MediaType:     "",
			Layers:        []*types.Layer{{MediaType: "", Size: len(buf.Bytes()), Digest: digest, SkynetLink: link, UUID: uuid}},
		},
	}

	err = r.localCache.Update([]byte(namespace), metadata.Bytes())
	if err != nil {
		errMsg := r.errorResponse(RegistryErrorCodeBlobUploadInvalid, err.Error(), nil)
		ctx.Set(types.HttpEndpointErrorKey, errMsg)
		r.logger.Log(ctx)
		return ctx.JSONBlob(http.StatusBadRequest, errMsg)
	}

	locationHeader := link
	ctx.Response().Header().Set("Location", locationHeader)
	return ctx.NoContent(http.StatusCreated)
}

// ChunkedUpload
// PATCH /v2/<name>/blobs/uploads/<uuid>
func (r *registry) ChunkedUpload(ctx echo.Context) error {
	return r.b.UploadBlob(ctx)
}

/*StartUpload
for postgres:
start a tnx
registry.tnxMap[uuid] = {txn,blobs[],timeout}
*/
// POST /v2/<name>/blobs/uploads/
func (r *registry) StartUpload(ctx echo.Context) error {
	ctx.Set(types.HandlerStartTime, time.Now())

	namespace := ctx.Param("username") + "/" + ctx.Param("imagename")
	clientDigest := ctx.QueryParam("digest")

	if clientDigest != "" {
		buf := &bytes.Buffer{}
		if _, err := io.Copy(buf, ctx.Request().Body); err != nil {
			details := map[string]interface{}{
				"clientDigest": clientDigest,
				"namespace":    namespace,
			}
			errMsg := r.errorResponse(
				RegistryErrorCodeBlobUploadInvalid,
				"error while reading request body",
				details,
			)

			ctx.Set(types.HttpEndpointErrorKey, errMsg)
			r.logger.Log(ctx)
			return ctx.JSONBlob(http.StatusNotFound, errMsg)
		}
		_ = ctx.Request().Body.Close() // why defer? body is already read :)
		dig := digest(buf.Bytes())

		if dig != clientDigest {
			details := map[string]interface{}{
				"clientDigest":   clientDigest,
				"computedDigest": dig,
			}
			errMsg := r.errorResponse(
				RegistryErrorCodeDigestInvalid,
				"client digest does not meet computed digest",
				details,
			)
			ctx.Set(types.HttpEndpointErrorKey, errMsg)
			r.logger.Log(ctx)
			return ctx.JSONBlob(http.StatusBadRequest, errMsg)
		}

		skylink, err := r.skynet.Upload(namespace, dig, buf.Bytes(), true)
		if err != nil {
			errMsg := r.errorResponse(RegistryErrorCodeBlobUploadInvalid, err.Error(), nil)
			ctx.Set(types.HttpEndpointErrorKey, errMsg)
			r.logger.Log(ctx)
			return ctx.JSONBlob(http.StatusRequestedRangeNotSatisfiable, errMsg)
		}

		layerV2 := &types.LayerV2{
			MediaType:   ctx.Request().Header.Get("content-type"),
			Digest:      dig,
			SkynetLink:  skylink,
			UUID:        uuid.NewString(),
			BlobDigests: nil,
			Size:        len(buf.Bytes()),
		}

		txnOp, err := r.store.NewTxn(ctx.Request().Context())
		if err != nil {
			errMsg := r.errorResponse(RegistryErrorCodeUnknown, err.Error(), nil)
			ctx.Set(types.HttpEndpointErrorKey, errMsg)
			r.logger.Log(ctx)
			return ctx.JSONBlob(http.StatusInternalServerError, errMsg)
		}

		if err := r.store.SetLayer(ctx.Request().Context(), txnOp, layerV2); err != nil {
			errMsg := r.errorResponse(RegistryErrorCodeBlobUploadInvalid, err.Error(), nil)
			ctx.Set(types.HttpEndpointErrorKey, errMsg)
			r.logger.Log(ctx)
			return ctx.JSONBlob(http.StatusBadRequest, errMsg)
		}
		if err := r.store.Commit(ctx.Request().Context(), txnOp); err != nil {
			errMsg := r.errorResponse(RegistryErrorCodeBlobUploadInvalid, err.Error(), nil)
			ctx.Set(types.HttpEndpointErrorKey, errMsg)
			r.logger.Log(ctx)
			return ctx.JSONBlob(http.StatusBadRequest, errMsg)
		}

		link := r.getHttpUrlFromSkylink(skylink)
		ctx.Response().Header().Set("Location", link)
		r.logger.Log(ctx)
		return ctx.NoContent(http.StatusCreated)
	}

	id := uuid.New()
	locationHeader := fmt.Sprintf("/v2/%s/blobs/uploads/%s", namespace, id.String())
	txn, err := r.store.NewTxn(ctx.Request().Context())
	if err != nil {
		errMsg := r.errorResponse(
			RegistryErrorCodeUnknown,
			err.Error(),
			nil,
		)
		ctx.Set(types.HttpEndpointErrorKey, errMsg)
		r.logger.Log(ctx)
		return ctx.JSONBlob(http.StatusInternalServerError, errMsg)
	}
	r.txnMap[id.String()] = TxnStore{
		txn:         txn,
		blobDigests: []string{},
		timeout:     time.Minute * 30,
	}
	ctx.Response().Header().Set("Location", locationHeader)
	ctx.Response().Header().Set("Content-Length", "0")
	ctx.Response().Header().Set("Docker-Upload-UUID", id.String())
	ctx.Response().Header().Set("Range", fmt.Sprintf("0-%d", 0))

	return ctx.NoContent(http.StatusAccepted)
}

//UploadProgress TODO
func (r *registry) UploadProgress(ctx echo.Context) error {
	ctx.Set(types.HandlerStartTime, time.Now())

	namespace := ctx.Param("username") + "/" + ctx.Param("imagename")
	uuid := ctx.Param("uuid")

	skylink, err := r.localCache.GetSkynetURL(namespace, uuid)
	if err != nil {
		locationHeader := fmt.Sprintf("/v2/%s/blobs/uploads/%s", namespace, uuid)
		ctx.Response().Header().Set("Location", locationHeader)
		ctx.Response().Header().Set("Range", "bytes=0-0")
		ctx.Response().Header().Set("Docker-Upload-UUID", uuid)

		return ctx.NoContent(http.StatusNoContent)
	}

	metadata, err := r.skynet.Metadata(skylink)
	if err != nil {
		locationHeader := fmt.Sprintf("/v2/%s/blobs/uploads/%s", namespace, uuid)
		ctx.Response().Header().Set("Location", locationHeader)
		ctx.Response().Header().Set("Range", "bytes=0-0")
		ctx.Response().Header().Set("Docker-Upload-UUID", uuid)

		return ctx.NoContent(http.StatusNoContent)
	}

	locationHeader := fmt.Sprintf("/v2/%s/blobs/uploads/%s", namespace, uuid)
	ctx.Response().Header().Set("Location", locationHeader)
	ctx.Response().Header().Set("Range", fmt.Sprintf("bytes=0-%d", metadata.ContentLength))
	ctx.Response().Header().Set("Docker-Upload-UUID", uuid)

	return ctx.NoContent(http.StatusNoContent)
}

// CompleteUpload
/*PUT /v2/<name>/blobs/uploads/<uuid>?digest=<digest>
for postgres:
this is where we insert into the layer after all the blobs have been accumulated
and inserted in the blob table
thus committing the txn
*/
func (r *registry) CompleteUpload(ctx echo.Context) error {
	ctx.Set(types.HandlerStartTime, time.Now())

	dig := ctx.QueryParam("digest")
	namespace := ctx.Param("username") + "/" + ctx.Param("imagename")
	id := ctx.Param("uuid")

	buf := &bytes.Buffer{}
	if _, err := io.Copy(buf, ctx.Request().Body); err != nil {
		errMsg := r.errorResponse(RegistryErrorCodeDigestInvalid, err.Error(), nil)
		ctx.Set(types.HttpEndpointErrorKey, errMsg)
		r.logger.Log(ctx)
		return ctx.JSONBlob(http.StatusBadRequest, errMsg)
	}
	_ = ctx.Request().Body.Close()
	// insert if bz is not nil
	ubuf := bytes.NewBuffer(r.b.uploads[id])
	ubuf.Write(buf.Bytes())
	ourHash := digest(ubuf.Bytes())
	delete(r.b.uploads, id)

	if ourHash != dig {
		details := map[string]interface{}{
			"headerDigest": dig, "serverSideDigest": ourHash, "bodyDigest": digest(buf.Bytes()),
		}
		errMsg := r.errorResponse(RegistryErrorCodeDigestInvalid, "digest mismatch", details)
		ctx.Set(types.HttpEndpointErrorKey, errMsg)
		r.logger.Log(ctx)
		return ctx.JSONBlob(http.StatusBadRequest, errMsg)
	}

	blobNamespace := fmt.Sprintf("%s/blobs", namespace)
	skylink, err := r.skynet.Upload(blobNamespace, dig, ubuf.Bytes(), true)
	if err != nil {
		errMsg := r.errorResponse(RegistryErrorCodeBlobUploadInvalid, err.Error(), nil)
		ctx.Set(types.HttpEndpointErrorKey, errMsg)
		r.logger.Log(ctx)
		return ctx.JSONBlob(http.StatusRequestedRangeNotSatisfiable, errMsg)
	}

	txnOp, ok := r.txnMap[id]
	layer := &types.LayerV2{
		MediaType:   "",
		Digest:      dig,
		SkynetLink:  skylink,
		UUID:        id,
		BlobDigests: txnOp.blobDigests,
		Size:        len(buf.Bytes()),
	}
	if !ok {
		errMsg := r.errorResponse(RegistryErrorCodeUnknown, "transaction does not exist for uuid -"+id, nil)
		ctx.Set(types.HttpEndpointErrorKey, errMsg)
		r.logger.Log(ctx)
		return ctx.JSONBlob(http.StatusBadRequest, errMsg)
	}

	if err := r.store.SetLayer(ctx.Request().Context(), txnOp.txn, layer); err != nil {
		errMsg := r.errorResponse(RegistryErrorCodeUnknown, err.Error(), echo.Map{
			"error_detail": "set layer issues",
		})
		ctx.Set(types.HttpEndpointErrorKey, errMsg)
		r.logger.Log(ctx)
		return ctx.JSONBlob(http.StatusBadRequest, errMsg)
	}

	if err := r.store.Commit(ctx.Request().Context(), txnOp.txn); err != nil {
		errMsg := r.errorResponse(RegistryErrorCodeUnknown, err.Error(), echo.Map{
			"error_detail": "commitment issue",
		})
		ctx.Set(types.HttpEndpointErrorKey, errMsg)
		r.logger.Log(ctx)
		return ctx.JSONBlob(http.StatusBadRequest, errMsg)
	}
	delete(r.txnMap, id)

	locationHeader := fmt.Sprintf("/v2/%s/blobs/%s", namespace, ourHash)
	ctx.Response().Header().Set("Content-Length", "0")
	ctx.Response().Header().Set("Docker-Content-Digest", ourHash)
	ctx.Response().Header().Set("Location", locationHeader)
	return ctx.NoContent(http.StatusCreated)
}

//BlobMount to be implemented by guacamole at a later stage
func (r *registry) BlobMount(ctx echo.Context) error {
	return nil
}

//PushImage is already implemented through StartUpload and ChunkedUpload
func (r *registry) PushImage(ctx echo.Context) error {
	return nil
}

func (r *registry) PushManifest(ctx echo.Context) error {
	ctx.Set(types.HandlerStartTime, time.Now())

	namespace := ctx.Param("username") + "/" + ctx.Param("imagename")
	ref := ctx.Param("reference")
	contentType := ctx.Request().Header.Get("Content-Type")

	var manifest ImageManifest

	buf := &bytes.Buffer{}
	_, err := io.Copy(buf, ctx.Request().Body)
	if err != nil {
		return ctx.JSON(http.StatusBadRequest, echo.Map{
			"error":   err.Error(),
			"message": "failed in push manifest while io Copy",
		})
	}
	_ = ctx.Request().Body.Close()

	err = json.Unmarshal(buf.Bytes(), &manifest)
	if err != nil {
		errMsg := r.errorResponse(RegistryErrorCodeBlobUnknown, err.Error(), nil)
		ctx.Set(types.HttpEndpointErrorKey, errMsg)
		r.logger.Log(ctx)
		return ctx.JSONBlob(http.StatusBadRequest, errMsg)
	}
	dig := digest(buf.Bytes())

	mfNamespace := fmt.Sprintf("%s/manifests", namespace)
	skylink, err := r.skynet.Upload(mfNamespace, dig, buf.Bytes(), true)
	if err != nil {
		errMsg := r.errorResponse(RegistryErrorCodeManifestBlobUnknown, err.Error(), nil)
		ctx.Set(types.HttpEndpointErrorKey, errMsg)
		r.logger.Log(ctx)
		return ctx.JSONBlob(http.StatusNotFound, errMsg)
	}

	var layerIDs []string
	for _, layer := range manifest.Layers {
		layerIDs = append(layerIDs, layer.Digest)
	}

	id := uuid.New()
	mfc := types.ConfigV2{
		UUID:      id.String(),
		Namespace: namespace,
		Reference: ref,
		Digest:    dig,
		Skylink:   skylink,
		MediaType: contentType,
		Layers:    layerIDs,
		Size:      0,
	}

	val := &types.ImageManifestV2{
		Uuid:          uuid.NewString(),
		Namespace:     namespace,
		MediaType:     "",
		SchemaVersion: 2,
	}

	txnOp, err := r.store.NewTxn(context.Background())
	if err != nil {
		errMsg := r.errorResponse(RegistryErrorCodeUnknown, err.Error(), echo.Map{
			"reason": "PG_ERR_CREATE_NEW_TXN",
		})
		ctx.Set(types.HttpEndpointErrorKey, errMsg)
		r.logger.Log(ctx)
		_ = r.store.Abort(ctx.Request().Context(), txnOp)
		return ctx.JSONBlob(http.StatusInternalServerError, errMsg)
	}

	if err := r.store.SetManifest(ctx.Request().Context(), txnOp, val); err != nil {
		errMsg := r.errorResponse(RegistryErrorCodeUnknown, err.Error(), nil)
		ctx.Set(types.HttpEndpointErrorKey, errMsg)
		r.logger.Log(ctx)
		_ = r.store.Abort(ctx.Request().Context(), txnOp)
		return ctx.JSONBlob(http.StatusBadRequest, errMsg)
	}

	if err := r.store.SetConfig(ctx.Request().Context(), txnOp, mfc); err != nil {
		errMsg := r.errorResponse(RegistryErrorCodeUnknown, err.Error(), nil)
		ctx.Set(types.HttpEndpointErrorKey, errMsg)
		r.logger.Log(ctx)
		_ = r.store.Abort(ctx.Request().Context(), txnOp)
		return ctx.JSONBlob(http.StatusBadRequest, errMsg)
	}

	if err = r.store.Commit(ctx.Request().Context(), txnOp); err != nil {
		errMsg := r.errorResponse(RegistryErrorCodeUnknown, err.Error(), echo.Map{
			"reason": "ERR_PG_COMMIT_TXN",
		})
		ctx.Set(types.HttpEndpointErrorKey, errMsg)
		r.logger.Log(ctx)
		_ = r.store.Abort(ctx.Request().Context(), txnOp)
		return ctx.JSONBlob(http.StatusInternalServerError, errMsg)
	}

	locationHeader := r.getHttpUrlFromSkylink(skylink)
	ctx.Response().Header().Set("Location", locationHeader)
	ctx.Response().Header().Set("Docker-Content-Digest", dig)
	ctx.Response().Header().Set("X-Docker-Content-ID", skylink)
	return ctx.String(http.StatusCreated, "Created")
}

// PushLayer
// POST /v2/<name>/blobs/uploads/
func (r *registry) PushLayer(ctx echo.Context) error {
	ctx.Set(types.HandlerStartTime, time.Now())

	elem := strings.Split(ctx.Request().URL.Path, "/")
	elem = elem[1:]
	if elem[len(elem)-1] == "" {
		elem = elem[:len(elem)-1]
	}
	// Must have a path of form /v2/{name}/blobs/{upload,sha256:}
	if len(elem) < 4 {
		errMsg := r.errorResponse(RegistryErrorCodeNameInvalid, "blobs must be attached to a repo", nil)
		ctx.Set(types.HttpEndpointErrorKey, errMsg)
		r.logger.Log(ctx)
		return ctx.JSONBlob(http.StatusNotFound, errMsg)
	}

	id := uuid.New()
	p := path.Join(elem[1 : len(elem)-2]...)
	locationHeader := fmt.Sprintf("/v2/%s/blobs/uploads/%s", p, id.String())
	ctx.Response().Header().Set("Location", locationHeader)
	ctx.Response().Header().Set("Docker-Upload-UUID", id.String())
	ctx.Response().Header().Set("Range", "bytes=0-0")

	return ctx.NoContent(http.StatusAccepted)
}

func (r *registry) CancelUpload(ctx echo.Context) error {
	return nil
}

// DeleteTagOrManifest
// DELETE /v2/<name>/manifest/<tag> or <digest>
func (r *registry) DeleteTagOrManifest(ctx echo.Context) error {
	ctx.Set(types.HandlerStartTime, time.Now())

	namespace := ctx.Param("username") + "/" + ctx.Param("imagename")
	ref := ctx.Param("reference")

	if ref == "" {
		reqURI := strings.Split(ctx.Request().RequestURI, "/")
		if len(reqURI) == 6 {
			ref = reqURI[5]
		}
	}
	txnOp, _ := r.store.NewTxn(context.Background())
	if err := r.store.DeleteManifestOrTag(ctx.Request().Context(), txnOp, ref); err != nil {
		//if err := r.localCache.UpdateManifestRef(namespace, ref); err != nil {
		details := map[string]interface{}{
			"namespace": namespace,
			"digest":    ref,
		}
		errMsg := r.errorResponse(RegistryErrorCodeManifestUnknown, err.Error(), details)
		ctx.Set(types.HttpEndpointErrorKey, errMsg)
		r.logger.Log(ctx)
		return ctx.JSONBlob(http.StatusNotFound, errMsg)
	}

	_ = r.store.Commit(ctx.Request().Context(), txnOp)
	return ctx.NoContent(http.StatusAccepted)
}

func (r *registry) DeleteLayer(ctx echo.Context) error {
	ctx.Set(types.HandlerStartTime, time.Now())

	dig := ctx.Param("digest")

	//var m types.Metadata

	layer, err := r.store.GetLayer(ctx.Request().Context(), dig)
	//_, err := r.localCache.GetDigest(dig)
	if err != nil {

		errMsg := r.errorResponse(RegistryErrorCodeBlobUnknown, err.Error(), nil)
		ctx.Set(types.HttpEndpointErrorKey, errMsg)
		r.logger.Log(ctx)
		return ctx.JSONBlob(http.StatusNotFound, errMsg)
	}
	blobs := layer.BlobDigests

	//err = r.localCache.DeleteLayer(namespace, dig)
	txnOp, _ := r.store.NewTxn(context.Background())
	err = r.store.DeleteLayerV2(ctx.Request().Context(), txnOp, dig)
	if err != nil {
		logMsg := echo.Map{
			"error":  err.Error(),
			"caller": "DeleteLayer",
		}

		bz, err := json.Marshal(logMsg)
		if err == nil {
			ctx.Set(types.HttpEndpointErrorKey, logMsg)
			r.logger.Log(ctx)
		}

		return ctx.JSONBlob(http.StatusInternalServerError, bz)
	}

	for i := range blobs {
		//if err = r.localCache.DeleteDigest(dig); err != nil {
		if err = r.store.DeleteBlobV2(ctx.Request().Context(), txnOp, blobs[i]); err != nil {
			logMsg := echo.Map{
				"error":  err.Error(),
				"caller": "DeleteLayer",
			}

			ctx.Set(types.HttpEndpointErrorKey, logMsg)
			r.logger.Log(ctx)
			bz, err := json.Marshal(logMsg)
			if err != nil {
				r.log.Err(err).Send()
			}

			return ctx.JSONBlob(http.StatusInternalServerError, bz)
		}
	}
	_ = r.store.Commit(ctx.Request().Context(), txnOp)
	return ctx.NoContent(http.StatusAccepted)
}

// Should also look into 401 Code
// https://docs.docker.com/registry/spec/api/
func (r *registry) ApiVersion(ctx echo.Context) error {

	ctx.Response().Header().Set(HeaderDockerDistributionApiVersion, "registry/2.0")
	return ctx.String(http.StatusOK, "OK\n")
}

func (r *registry) GetImageNamespace(ctx echo.Context) error {

	searchQuery := ctx.QueryParam("search_query")
	if searchQuery == "" {
		return ctx.JSON(http.StatusBadRequest, echo.Map{
			"error": "search query must not be empty",
		})
	}
	result, err := r.store.GetImageNamespace(ctx.Request().Context(), searchQuery)
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, echo.Map{
			"error":   err.Error(),
			"message": "error getting image namespace",
		})
	}
	return ctx.JSON(http.StatusOK, result)
}
