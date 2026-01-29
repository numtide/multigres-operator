### Options to manage poolers (database nodes)

You have three paths to achieve zero-downtime upgrades:

1. Keep `StatefulSet` but override its default behavior (`OnDelete`).
2. Abandon `StatefulSet` and build a Custom Controller that manages Pods/PVCs directly.
3. Use **OpenKruise**, an advanced workload controller that extends StatefulSet capabilities.

---

### **Option A: The StatefulSet Route (OnDelete)**

*Leveraging Kubernetes primitives while taking manual control of the rollout timing.*

#### **1. How it Works Exactly**

* **Configuration:** You modify `pool_statefulset.go` to set the Update Strategy to `OnDelete`.
```go
UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
    Type: appsv1.OnDeleteStatefulSetStrategyType,
}

```


* *Result:* When you update the image (e.g., `postgres:15` -> `16`), Kubernetes updates the `revision` hash but **touches nothing**. The pods remain on the old version indefinitely.


* **The "Smart Roll" Loop (`shard_controller.go`):**
Your Operator runs a reconciliation loop to manually orchestrate the upgrade:
1. **Discovery:** It queries `MultiOrch` (or TopoServer) to identify the **Primary** (e.g., `pool-0`) and **Replicas** (e.g., `pool-1`, `pool-2`).
2. **Upgrade Replicas First:**
* The Operator identifies a Replica (e.g., `pool-2`) running an old revision.
* **Action:** `client.Delete(pod)`
* **Reaction:** The K8s StatefulSet controller sees a missing pod and immediately recreates it using the **new** revision (new image).
* **Wait:** The Operator pauses until `pool-2` is Ready and synced.


3. **Controlled Switchover:**
* Once all replicas are upgraded, the Operator calls `MultiOrch.Switchover(shardID)`.
* `MultiOrch` gracefully demotes `pool-0` and promotes a synced replica (e.g., `pool-1`).


4. **Upgrade Old Primary:**
* Now that `pool-0` is a replica, the Operator deletes it.
* K8s recreates it with the new image.





#### **2. Scaling Constraints**

* **Strict LIFO (Last-In, First-Out):** You are bound by Kubernetes ordinal rules. If you have 3 pods and want to scale down to 2, you **must** delete `pool-2`.
* **Impact:** If `pool-1` is on a dying node, you cannot just delete `pool-1`. You must effectively "fail over" processes away from `pool-2`, delete it, and then rely on K8s to eventually heal `pool-1`.

---

### **Option B: The Custom Pod Controller (Direct Management)**

*Re-implementing lifecycle logic to treat database nodes as independent resources.*

#### **1. How it Works Exactly**

* **Definition:** You delete `pool_statefulset.go`. The Operator calculates "Desired State" (e.g., "I need 3 replicas") vs "Current State" and reconciles the difference manually.
* **Resource Creation:**
* The Operator manually creates `PersistentVolumeClaim` objects with deterministic names (e.g., `data-zone-a-uid123`).
* The Operator manually creates `Pod` objects that mount these specific PVCs.
* Identity is handled via Labels (`multigres.com/shard=0`), not ordinals (`-0`, `-1`).


* **The "Smart Roll" Loop:**
1. **Detection:** The Operator sees `pod-uid-abc` has an old image.
2. **Upgrade Strategy:** You have two choices here:
* *Strategy B1 (In-Place Restart):* Delete `pod-uid-abc`. Immediately create `pod-uid-abc` (new image) and force it to bind to the **existing** PVC. (Fast, Minutes).
* *Strategy B2 (Blue/Green):* Create a **new** `pod-uid-xyz` with a **fresh** PVC. Clone data from Primary. Once synced, kill the old pod. (Slow, Hours).


3. **Switchover:** Performed via `MultiOrch` just like Option A.



#### **2. Scaling Flexibility**

* **Arbitrary Targeting:** You are not bound by LIFO.
* **Scenario:** A specific node in `us-east-1a` has a corrupted disk or network partition.
* **Action:** You simply delete that specific Pod/PVC. The Operator sees `Current < Desired` and spins up a fresh replacement on a healthy node.

---

### **Option C: OpenKruise (The "Super StatefulSet" Extension)**

*Using an advanced open-source workload controller (AdvancedStatefulSet) designed to fix standard StatefulSet limitations.*

#### **1. How it Works Exactly**

* **Configuration:** You install the OpenKruise CRDs in the cluster. You change `pool_statefulset.go` to generate an `apps.kruise.io/v1beta1` `StatefulSet` instead of the standard K8s one.
* **Update Strategy:** You enable the **In-Place Update** feature.
* *Mechanism:* When you update the image tag, OpenKruise does **not** delete the Pod. Instead, it stops the container, pulls the new image, and restarts the container **inside the same Pod sandbox**.
* *Result:* The Pod IP address does not change. The volume remains mounted. Restart time is reduced to seconds (process restart) rather than minutes (scheduling + volume attach).


* **The "Smart Roll" Loop:**
* **The Safety Gap:** Even with In-Place updates, restarting the Primary container **kills the database process**. You still face the exact same safety issue as Option A.
* **The Workflow:** You must still implement the same reconciliation loop as Option A:
1. Detect pending update (OpenKruise exposes this status).
2. Call `MultiOrch.Switchover` if the pending pod is Primary.
3. Signal OpenKruise to proceed with the update (via `partition` manipulation or `paused` fields).





#### **2. Scaling Flexibility**

* **Reserve Ordinals:** OpenKruise solves the LIFO problem.
* **Scenario:** `pool-1` is on a bad node, but you want to keep `pool-2`.
* **Action:** You update the AdvancedStatefulSet spec to add `pool-1` to the `reserveOrdinals` list.
* **Result:** OpenKruise deletes specifically `pool-1`, scales the set size down, but keeps `pool-0` and `pool-2` running.

---

### **Comparison Checklist: Pros & Cons**

| Feature | Option A: StatefulSet (OnDelete) | Option B: Custom Controller (Reuse PVC) | Option C: OpenKruise |
| --- | --- | --- | --- |
| **Development Complexity** | **Low.** K8s handles Pod scheduling, PVC binding, and stable network IDs for you. You just control the "delete" button. | **Very High.** You must re-invent basic K8s features: PVC binding, orphan garbage collection, deterministic naming, and anti-affinity. | **Medium.** You swap the API, but you still must write the Operator safety loop. |
| **Upgrade Safety** | **High.** With `OnDelete` + `MultiOrch` Switchover, you guarantee the Primary is never killed blindly. | **High.** You have total control, but you own the risk of bugs in your pod-creation logic (e.g., creating 2 primaries by mistake). | **High.** Same safety logic required as Option A. |
| **Scale Down Flexibility** | **Poor (LIFO Only).** You must always delete the highest ordinal (`pool-2`). You cannot surgically remove a faulty middle node (`pool-1`). | **Perfect.** You can delete any specific pod based on health, zone, or hardware issues. | **Perfect.** Can target specific ordinals to delete. |
| **Upgrade Speed** | **Fast (Minutes).** Pod Re-creation (Scheduling + Volume Attach). | **Fast (Minutes).** Same as Option A if reusing PVC. | **Very Fast (Seconds).** Container restart only. No volume detach/attach. |
| **Storage Reliability** | **Medium.** Prone to "Stuck Volume" errors if K8s tries to move a pod to a new node quickly. | **Medium.** Same risks as Option A if reusing PVCs. | **High.** Volume never detaches during update. |
| **Maintenance Burden** | **Low.** You rely on standard, battle-tested K8s behavior maintained by the community. | **High.** You effectively maintain your own "StatefulSet Controller" code forever. | **Medium.** You must install/maintain OpenKruise CRDs in every cluster. |
| **Dependencies** | **None.** Works on vanilla K8s. | **None.** Pure Go code. | **Heavy.** Requires installing OpenKruise controller manager. |
