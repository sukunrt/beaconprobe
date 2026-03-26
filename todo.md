- Want to be absolutely sure that late validators are late because they're sending their votes late

- The mesh size doesn't matter much. We are within 5 hops of everyone. 

- log the peers location too

- Despite all of this, is the mesh still slower than we'd expect? 
    - The mesh is now reasonably in line with our model.
    - model suggests 500ms => 75%ile
    - our system 500ms => 60%ile
    - most importantly higher percentiles cannot be mapped because they're all nodes that are voting just really late it's not on "gossipsub"

- Now: 
    - Let's triangulate the mesh:
    - If we run 10 nodes. We can expect to be close to the source of the message. What we can do is see when we first get the message to the spread across the 10 observers and see how fast the message is propagating.  


## Findings:
- Some entities consistently slow
- Too much IHAVE Traffic
- Too many IWANTs too, need to use slot sessioning.
- The crawler is really slow in Prysm. We much benchmark this number. 
- Modify prysm to test crawler as an empty validator. We want a mode where we pretend to be a validator. 
- segregate peers by rtt! This will probably give us a good set of mesh propagation time. 
