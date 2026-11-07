# Reach

AgentMesh is the live app at https://www.agent-mesh.app. About 100 to 200 people use it today. They build and run workflows there.

They use the Arkiv part when they share a workflow. The author is signed in and posts the graph. The recipient opens `/s/<entityKey>` and the page reads the entity from Rabdi. Nobody else hits Arkiv. Payments stay on Whitechain or Arc.

It is not on `master` yet. It ships to those users after Mumbai when `feat/whitechain-x402` is what https://www.agent-mesh.app runs, and when `ARKIV_SHARE_PRIVATE_KEY` is set. Until that key is set, `POST /shares` returns `share_disabled` and no new link is written. Links already on Rabdi keep opening from the browser until they expire, without our database.
