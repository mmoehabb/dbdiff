package diff

const (
	InsertData OperationType = "insert_data"
	UpdateData OperationType = "update_data"
	DeleteData OperationType = "delete_data"
)

type InsertDataOperation struct {
	SchemaName string
	TableName  string
	Row        map[string]interface{}
}

func (o InsertDataOperation) OperationType() OperationType { return InsertData }
func (o InsertDataOperation) IsDestructive() bool          { return false }

type UpdateDataOperation struct {
	SchemaName string
	TableName  string
	PrimaryKey map[string]interface{}
	Updates    map[string]interface{}
}

func (o UpdateDataOperation) OperationType() OperationType { return UpdateData }
func (o UpdateDataOperation) IsDestructive() bool          { return false }

type DeleteDataOperation struct {
	SchemaName string
	TableName  string
	PrimaryKey map[string]interface{}
}

func (o DeleteDataOperation) OperationType() OperationType { return DeleteData }
func (o DeleteDataOperation) IsDestructive() bool          { return true }
