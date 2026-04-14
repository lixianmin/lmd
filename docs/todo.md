
This file records temporary development thoughts. When processed, items are resolved and removed. When empty (except this header), all items have been addressed.



2. 我们在修复 Bug 或进行各种测试的过程中，需要不断地删除和添加文档。

我不希望在整个过程中，手动去删除和添加所有的 Collection。所以请你参考一下 QMD 这个项目有没有相关的方案，比如进行 Review 的方案，或者是进行 Reindex 等方法。

我希望能够直接通过命令行参数来重做这件事。在不修改 Collection 的情况下，效果上相当于删除了 Collection，又重新加入了一遍。

我希望 QMD 里面有没有一个参照的命令行参数，能够直接用来注做这个事情？如果没有的话，我们自己加一个