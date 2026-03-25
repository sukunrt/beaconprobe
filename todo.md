- Want to be absolutely sure that late validators are late because they're sending their votes late

- The mesh size doesn't matter much. We are within 5 hops of everyone. 

- log the peers location too

- Despite all of this, is the mesh still slower than we'd expect? 
  - If some peers were consistently slow, then we'd expect the 50%ile to match us exactly. Is that matching? 
  - for a node in india: 80%ile is 500ms from europe min and 250ms from europe max
  - all 3 europe nodes converge around 80%ile very quickly. This number is 750 - 850ms. 
  - the similarity of deciles suggests that we've probably picked the same peers in all the meshes. 
  - we should at least get the first 50%ile of the votes asap. 


