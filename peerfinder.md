# peer finder logic

Currently we crawl the network + get peers from a bootstrap list. But the backpressure is dropping peers which is very inefficient. 

Instead we'd like to now enqueue amount as much as we can in a peerFinder struct
which encapsulates the routebootstrappeers and crawling logic. This struct will do the routing and the peermanager will call it to get peers to dial

Currently the crawling logic pushes to the dialer. 
Now it'll push to this struct. This will store all peers and do the claiming / routing etc. 
The peer manager will get peers to dial from this struct. 


peerRouter.AddPeer(enr, peer // whatever)
peerRouter.GetPeers(peerID(nodeID), count) 
  return count random peers
  
This ensures we never drop any peers.
