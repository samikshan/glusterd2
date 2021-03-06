package peercommands

import (
	"net/http"

	"github.com/gluster/glusterd2/etcdmgmt"
	"github.com/gluster/glusterd2/gdctx"
	"github.com/gluster/glusterd2/peer"
	"github.com/gluster/glusterd2/rest"
	"github.com/gluster/glusterd2/utils"

	log "github.com/Sirupsen/logrus"
	"github.com/gorilla/mux"
)

func deletePeerHandler(w http.ResponseWriter, r *http.Request) {

	// FIXME: This is not txn based, yet. Behaviour when multiple simultaneous
	// delete peer requests are sent to same node is unknown.

	peerReq := mux.Vars(r)

	id := peerReq["peerid"]
	if id == "" {
		rest.SendHTTPError(w, http.StatusBadRequest, "peerid not present in the request")
		return
	}
	// Check whether the member exists
	p, e := peer.GetPeerF(id)
	if e != nil || p == nil {
		rest.SendHTTPError(w, http.StatusNotFound, "peer not found in cluster")
		return
	}

	// Removing self should be disallowed (like in glusterd1)
	if id == gdctx.MyUUID.String() {
		rest.SendHTTPError(w, http.StatusBadRequest, "Removing self is disallowed.")
		return
	}

	remotePeerAddress, err := utils.FormRemotePeerAddress(p.Addresses[0])
	if err != nil {
		rest.SendHTTPError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Validate whether the peer can be deleted
	rsp, e := ValidateDeletePeer(remotePeerAddress, id)
	if e != nil {
		rest.SendHTTPError(w, http.StatusInternalServerError, rsp.OpError)
		return
	}

	// Remove the peer from the store
	if e := peer.DeletePeer(id); e != nil {
		log.WithFields(log.Fields{
			"er":   e,
			"peer": id,
		}).Error("Failed to remove peer from the store")
		rest.SendHTTPError(w, http.StatusInternalServerError, e.Error())
	} else {
		rest.SendHTTPResponse(w, http.StatusNoContent, nil)
	}

	// Delete member from etcd cluster
	e = etcdmgmt.EtcdMemberRemove(p.MemberID)
	if e != nil {
		log.WithFields(log.Fields{
			"er":   e,
			"peer": id,
		}).Error("Failed to remove member from etcd cluster")

		rest.SendHTTPError(w, http.StatusInternalServerError, e.Error())
		return
	}

	// Remove data dir of etcd on remote machine. Restart etcd on remote machine
	// in standalone (single cluster) mode.
	var etcdConf EtcdConfigReq
	etcdConf.DeletePeer = true
	etcdrsp, e := ConfigureRemoteETCD(remotePeerAddress, &etcdConf)
	if e != nil {
		log.WithField("err", e).Error("Failed to configure remote etcd.")
		rest.SendHTTPError(w, http.StatusInternalServerError, etcdrsp.OpError)
		return
	}
}
